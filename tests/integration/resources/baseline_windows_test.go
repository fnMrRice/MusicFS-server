//go:build windows

package resources_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
	"golang.org/x/sys/windows"
	_ "modernc.org/sqlite"
)

func isGone(err error) bool { return os.IsNotExist(err) }

type resourceReport struct {
	MeasuredAt               time.Time         `json:"measuredAt"`
	Platform                 string            `json:"platform"`
	GoVersion                string            `json:"goVersion"`
	SQLiteVersion            string            `json:"sqliteVersion"`
	BinarySHA256             map[string]string `json:"binarySha256"`
	LogicalCPUs              int               `json:"logicalCPUs"`
	RootPID                  int               `json:"rootPid"`
	SampleIntervalMillis     int               `json:"sampleIntervalMs"`
	SyntheticFiles           int               `json:"syntheticFiles"`
	BytesPerFile             int               `json:"bytesPerFile"`
	HashBudgetBytesPerSecond int               `json:"hashBudgetBytesPerSecond"`
	CacheBudgetBytes         int               `json:"cacheBudgetBytes"`
	CompletedOutputBytes     int64             `json:"completedOutputBytes"`
	CompletedOutputSHA256    string            `json:"completedOutputSha256"`
	RetainedOutputBytes      int64             `json:"retainedOutputBytes"`
	Outcomes                 []outcome         `json:"outcomes"`
	Samples                  []sample          `json:"samples"`
	Errors                   []string          `json:"measurementErrors"`
}

// This opt-in measurement is deliberately not a machine-dependent performance
// threshold. It starts only owned processes and uses disposable synthetic audio.
func TestResourceBaselineActualServer(t *testing.T) {
	if os.Getenv("MUSICFS_RESOURCE_BASELINE") != "1" {
		t.Skip("set MUSICFS_RESOURCE_BASELINE=1 to measure actual server resources")
	}
	for _, name := range []string{"MUSICFS_FFMPEG", "MUSICFS_FFPROBE", "MUSICFS_FPCALC", "MUSICFS_RESOURCE_REPORT"} {
		if os.Getenv(name) == "" {
			t.Fatal("required environment variable missing:", name)
		}
	}
	f := startServer(t)
	c := login(t, f.base, f.token)
	var status lifecycle.Status
	check(t, c.request("GET", "/lifecycle", nil, "", &status))
	var lease lifecycle.Lease
	check(t, c.request("POST", "/lifecycle/leases", lifecycle.AcquireRequest{InstanceID: status.InstanceID, ClientID: "resource-baseline-client"}, "resource-baseline-lease", &lease))
	stopRenew, renewDone := make(chan struct{}), make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for sequence := int64(1); ; sequence++ {
			select {
			case <-stopRenew:
				renewDone <- nil
				return
			case <-ticker.C:
			}
			if err := c.request("POST", "/lifecycle/leases/"+lease.ID+"/renew", lifecycle.RenewRequest{InstanceID: status.InstanceID, Sequence: sequence}, "", nil); err != nil {
				renewDone <- err
				return
			}
		}
	}()
	defer close(stopRenew)
	tracks := scan(t, c)
	if len(tracks) != 32 {
		t.Fatalf("expected 32 synthetic sources, got %d", len(tracks))
	}
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].DisplayName < tracks[j].DisplayName })
	m := newMonitor(f)
	monitorFinished := false
	defer func() {
		if !monitorFinished {
			m.finish()
		}
	}()
	time.Sleep(3 * time.Second)
	report := resourceReport{MeasuredAt: time.Now().UTC(), Platform: runtime.GOOS + "/" + runtime.GOARCH, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), RootPID: f.pid, SampleIntervalMillis: 200, SyntheticFiles: 32, BytesPerFile: 44 + 180*48000*2, HashBudgetBytesPerSecond: 4 << 20, CacheBudgetBytes: 128 << 20}
	report.BinarySHA256 = f.binaries
	db, err := sql.Open("sqlite", ":memory:")
	check(t, err)
	check(t, db.QueryRow("SELECT sqlite_version()").Scan(&report.SQLiteVersion))
	check(t, db.Close())
	for cycle := 0; cycle < 4; cycle++ {
		m.setPhase(fmt.Sprintf("load-%d", cycle+1))
		batch := tracks[cycle*8 : (cycle+1)*8]
		jobs := queueWork(t, c, batch, cycle)
		consumers := startConsumers(t, c, batch, cycle)
		time.Sleep(3 * time.Second)
		m.setPhase(fmt.Sprintf("cancel-%d", cycle+1))
		result := cancelWork(t, c, jobs)
		result.StreamingBytes = stopConsumers(t, c, consumers)
		report.Outcomes = append(report.Outcomes, result)
		m.setPhase(fmt.Sprintf("settled-%d", cycle+1))
		time.Sleep(8 * time.Second)
		cache, err := directoryBytes(f.cache)
		check(t, err)
		if cache > int64(report.CacheBudgetBytes) {
			t.Fatalf("stopped playback exceeds cache budget: %d bytes", cache)
		}
		// Completed output may remain in the bounded idle cache. Cancelled
		// partial representations must not remain after the settling interval.
		check(t, filepath.WalkDir(f.cache, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && filepath.Ext(path) == ".part" {
				return fmt.Errorf("partial representation was not reclaimed")
			}
			return nil
		}))
		processes, err := processTree(uint32(f.pid))
		check(t, err)
		if len(processes) != 2 {
			t.Fatalf("unexpected remaining server process tree: %+v", processes)
		}
		assertSourceHandlesReleased(t, f.music)
	}
	m.setPhase("complete-output")
	report.CompletedOutputBytes, report.CompletedOutputSHA256 = completeOutput(t, c, tracks[0].ID)
	retained, err := directoryBytes(filepath.Join(f.cache, "playback", "processed"))
	check(t, err)
	if retained < report.CompletedOutputBytes || retained > int64(report.CacheBudgetBytes) {
		t.Fatalf("complete output retention is outside its budget: %d", retained)
	}
	report.RetainedOutputBytes = retained
	m.setPhase("idle-35s")
	time.Sleep(35 * time.Second)
	m.finish()
	monitorFinished = true
	report.Samples, report.Errors = m.samples, m.errors
	if len(report.Errors) > 0 {
		t.Fatalf("resource measurements incomplete: %v", report.Errors)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	check(t, err)
	path, err := filepath.Abs(os.Getenv("MUSICFS_RESOURCE_REPORT"))
	check(t, err)
	check(t, os.MkdirAll(filepath.Dir(path), 0700))
	check(t, os.WriteFile(path, append(data, '\n'), 0600))
	select {
	case e := <-renewDone:
		check(t, e)
		t.Fatal("lease renewer stopped early")
	default:
	}
	check(t, c.request("POST", "/lifecycle/leases/"+lease.ID+"/release", lifecycle.ReleaseRequest{InstanceID: status.InstanceID}, "", nil))
	select {
	case e := <-f.done:
		check(t, e)
	case <-time.After(10 * time.Second):
		t.Fatal("actual server did not stop cleanly")
	}
	t.Logf("resource report: %s; %d samples; outcomes: %+v", path, len(report.Samples), report.Outcomes)
}

func assertSourceHandlesReleased(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	check(t, err)
	for _, entry := range entries {
		path, err := windows.UTF16PtrFromString(filepath.Join(directory, entry.Name()))
		check(t, err)
		// Share=0 conflicts with any surviving source reader. No bytes change;
		// this directory contains only the fixture's own temporary audio.
		handle, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		check(t, err)
		check(t, windows.CloseHandle(handle))
	}
}

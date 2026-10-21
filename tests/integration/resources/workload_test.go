package resources_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

type background struct{ hash, fingerprint []string }
type outcome struct {
	HashStatuses        map[string]int `json:"hashStatuses"`
	FingerprintStatuses map[string]int `json:"fingerprintStatuses"`
	HashBytes           int64          `json:"hashBytes"`
	StreamingBytes      []int64        `json:"streamingBytes"`
	CancellationSeconds float64        `json:"cancellationSeconds"`
}

func queueWork(t *testing.T, c *client, tracks []library.Track, cycle int) background {
	t.Helper()
	jobs := background{}
	for i, track := range tracks {
		for _, kind := range []string{"hash", "fingerprint"} {
			input := map[string]any{}
			if kind == "fingerprint" {
				input["startMs"], input["durationMs"] = 0, 120000
			}
			var accepted struct {
				ID string `json:"jobId"`
			}
			check(t, c.request("POST", "/source-tracks/"+track.ID+"/"+kind+"-jobs", input, fmt.Sprintf("resource-%s-%d-%d", kind, cycle, i), &accepted))
			if kind == "hash" {
				jobs.hash = append(jobs.hash, accepted.ID)
			} else {
				jobs.fingerprint = append(jobs.fingerprint, accepted.ID)
			}
		}
	}
	return jobs
}

func cancelWork(t *testing.T, c *client, jobs background) outcome {
	t.Helper()
	o := outcome{HashStatuses: map[string]int{}, FingerprintStatuses: map[string]int{}}
	start := time.Now()
	for _, id := range jobs.hash {
		var job identification.HashJob
		check(t, c.request("POST", "/hash-jobs/"+id+"/cancel", struct{}{}, "", &job))
		o.HashStatuses[job.Status]++
		o.HashBytes += job.Progress.CompletedBytes
		if job.Status != "succeeded" && job.Status != "cancelled" {
			t.Fatalf("hash job failed: %+v", job)
		}
	}
	for _, id := range jobs.fingerprint {
		var job identification.FingerprintJob
		check(t, c.request("POST", "/fingerprint-jobs/"+id+"/cancel", struct{}{}, "", &job))
		o.FingerprintStatuses[job.Status]++
		if job.Status != "succeeded" && job.Status != "cancelled" {
			t.Fatalf("fingerprint job failed: %+v", job)
		}
	}
	o.CancellationSeconds = time.Since(start).Seconds()
	return o
}

type consumer struct {
	description playback.Description
	cancel      context.CancelFunc
	done        chan error
	bytes       atomic.Int64
}

func startConsumers(t *testing.T, c *client, tracks []library.Track, cycle int) []*consumer {
	t.Helper()
	result := []*consumer{}
	for i := 0; i < 4; i++ {
		request := playback.CreateRequest{SourceTrackID: tracks[i].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}, {MIME: "audio/mpeg", CanPlay: "probably"}}}, Preferences: playback.Preferences{ForceProcessing: i >= 2, GainDB: 0}}
		if i >= 2 {
			request.Preferences.GainDB = -6
		}
		s := &consumer{done: make(chan error, 1)}
		check(t, c.request("POST", "/playback/sessions", request, fmt.Sprintf("resource-play-%d-%d", cycle, i), &s.description))
		if (i < 2 && s.description.DeliveryMode != "proxy") || (i >= 2 && s.description.DeliveryMode != "processed") {
			t.Fatalf("wrong actual playback mode: %+v", s.description)
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		t.Cleanup(cancel)
		transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err == nil {
				if tcp, ok := conn.(*net.TCPConn); ok {
					err = tcp.SetReadBuffer(16 << 10)
					if err != nil {
						conn.Close()
					}
				}
			}
			return conn, err
		}}
		httpClient := &http.Client{Jar: c.http.Jar, Transport: transport}
		r, err := http.NewRequestWithContext(ctx, "GET", c.base+s.description.StreamURL, nil)
		check(t, err)
		response, err := httpClient.Do(r)
		check(t, err)
		if response.StatusCode != 200 {
			b, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("stream failed: %d %s", response.StatusCode, b)
		}
		go func() {
			defer response.Body.Close()
			defer transport.CloseIdleConnections()
			buffer := make([]byte, 8192)
			for {
				n, e := response.Body.Read(buffer)
				s.bytes.Add(int64(n))
				if e != nil {
					s.done <- e
					return
				}
				select {
				case <-ctx.Done():
					s.done <- ctx.Err()
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}()
		result = append(result, s)
	}
	return result
}

func stopConsumers(t *testing.T, c *client, consumers []*consumer) []int64 {
	t.Helper()
	result := []int64{}
	for _, s := range consumers {
		check(t, c.request("DELETE", "/playback/sessions/"+s.description.SessionID, nil, "", nil))
		s.cancel()
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			t.Fatal("stream did not cancel")
		}
		if s.bytes.Load() <= 8192 {
			t.Fatal("stream did not deliver actual audio")
		}
		result = append(result, s.bytes.Load())
	}
	return result
}

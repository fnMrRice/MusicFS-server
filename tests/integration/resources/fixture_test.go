package resources_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

type fixture struct {
	pid                              int
	base, token, cache, media, music string
	done                             <-chan error
	cancel                           context.CancelFunc
	binaries                         map[string]string
}

func startServer(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	music := filepath.Join(dir, "synthetic audio")
	check(t, os.Mkdir(music, 0700))
	// CC0 synthetic PCM; neither private music nor downloaded media is read.
	for i := 0; i < 32; i++ {
		check(t, os.WriteFile(filepath.Join(music, fmt.Sprintf("synthetic-%02d.wav", i)), wave(180, i), 0600))
	}
	root, err := filepath.Abs("../../..")
	check(t, err)
	build := func(name string) string {
		path := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		cmd := exec.Command("go", "build", "-mod=readonly", "-buildvcs=false", "-o", path, testkit.ProgramPackage(name))
		cmd.Dir = root
		if output, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("build %s: %v %s", name, e, output)
		}
		return path
	}
	server, provider, audio := build("musicfs"), build("musicfs-plugin-local"), build("musicfs-plugin-audio-ffmpeg")
	binaries := map[string]string{}
	for name, path := range map[string]string{"musicfs": server, "musicfs-plugin-local": provider, "musicfs-plugin-audio-ffmpeg": audio, "ffmpeg": os.Getenv("MUSICFS_FFMPEG"), "ffprobe": os.Getenv("MUSICFS_FFPROBE"), "fpcalc": os.Getenv("MUSICFS_FPCALC")} {
		data, err := os.ReadFile(path)
		check(t, err)
		binaries[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	check(t, err)
	address := listener.Addr().String()
	check(t, listener.Close())
	f := fixture{base: "http://" + address, token: domain.NewID(), cache: filepath.Join(dir, "cache"), media: filepath.Join(dir, "media"), music: music, binaries: binaries}
	fpbytes, err := os.ReadFile(os.Getenv("MUSICFS_FPCALC"))
	check(t, err)
	configuration := map[string]any{
		"configVersion": 1, "listen": address, "allowLoopbackHttp": true, "allowedOrigins": []string{f.base},
		"dataRoot": filepath.Join(dir, "data"), "mediaRoot": f.media, "cacheRoot": f.cache,
		"maxSessions": 8, "maxProcessing": 2, "maxCacheBytes": 128 << 20,
		"hashing":        map[string]any{"auto": false, "maxConcurrent": 1, "bytesPerSecond": 4 << 20},
		"fingerprinting": map[string]any{"enabled": true, "fpcalcPath": os.Getenv("MUSICFS_FPCALC"), "fpcalcSha256": fmt.Sprintf("%x", sha256.Sum256(fpbytes)), "maxConcurrent": 1, "timeoutSeconds": 30},
		"plugins": []any{
			map[string]any{"pluginId": "musicfs.plugin.source.localfiles", "executable": provider, "enabled": true, "instances": []any{
				map[string]any{"instanceId": "local", "role": "provider", "enabled": true, "config": map[string]any{"musicRoots": []any{map[string]any{"id": "local", "name": "Synthetic audio", "path": music}}}},
			}},
			map[string]any{"pluginId": "musicfs.plugin.decoder.ffmpeg", "executable": audio, "enabled": true, "instances": []any{
				map[string]any{"instanceId": "audio", "role": "audio", "enabled": true, "config": map[string]any{"ffmpegPath": os.Getenv("MUSICFS_FFMPEG"), "ffprobePath": os.Getenv("MUSICFS_FFPROBE")}},
			}},
		},
	}
	data, err := yaml.Marshal(configuration)
	check(t, err)
	path := filepath.Join(dir, "server.yaml")
	check(t, os.WriteFile(path, data, 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	f.cancel = cancel
	cmd := exec.CommandContext(ctx, server, "serve", "--config", path, "--startup-mode", "client-managed")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "MUSICFS_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "MUSICFS_ADMIN_TOKEN="+f.token)
	log, err := os.Create(filepath.Join(dir, "server.log"))
	check(t, err)
	cmd.Stdout, cmd.Stderr = log, log
	check(t, cmd.Start())
	f.pid = cmd.Process.Pid
	done := make(chan error, 1)
	f.done = done
	go func() { done <- cmd.Wait(); close(done); _ = log.Close() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("owned server did not stop")
		}
	})
	for deadline := time.Now().Add(20 * time.Second); ; {
		response, e := http.Get(f.base + "/api/v1/health/ready")
		if e == nil {
			_ = response.Body.Close()
			if response.StatusCode == 200 {
				return f
			}
		}
		select {
		case e := <-done:
			b, _ := os.ReadFile(log.Name())
			t.Fatalf("server exited: %v %s", e, b)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("server readiness timed out")
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func wave(seconds, seed int) []byte {
	b := make([]byte, 44+seconds*48000*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 96000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	for i := 0; i < seconds*48000; i++ {
		at := float64(i) / 48000
		step := int(at*4) + seed
		hz := 220 * math.Pow(2, float64((step*7+step/8)%24)/12)
		x := .18*(.7+.3*math.Sin(2*math.Pi*3*at))*math.Sin(2*math.Pi*hz*at) + .09*math.Sin(2*math.Pi*(110+float64(step%7)*31)*at)
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(x*32767)))
	}
	return b
}

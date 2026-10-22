package playback_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	streamhttp "github.com/fnMrRice/MusicFS-server/src/playback/stream"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPlaybackStreamSurvivesBrowserReadPause(t *testing.T) {
	for _, mode := range []string{"proxy", "processed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, description, expected := backpressureFixture(t, mode)
			connection, response, pending, done := stalledPlaybackConnection(t, f, description)
			waitForBlockedPlaybackWrite(t, pending, done)
			// 超过旧的 15 秒写超时，模拟浏览器缓冲充足后暂停网络读取。
			select {
			case err := <-done:
				t.Fatalf("playback ended while the browser paused reading: %v", err)
			case <-time.After(16 * time.Second):
			}
			if err := connection.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				t.Fatal(err)
			}
			actual, err := io.ReadAll(response.Body)
			if err != nil || !bytes.Equal(actual, expected) {
				t.Fatalf("playback did not resume with the complete representation: got %d, want %d, error %v", len(actual), len(expected), err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal("resumed playback failed", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("resumed playback did not finish")
			}
		})
	}
}

func TestPlaybackStopInterruptsBlockedNetworkWrite(t *testing.T) {
	for _, mode := range []string{"proxy", "processed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, description, _ := backpressureFixture(t, mode)
			_, _, pending, done := stalledPlaybackConnection(t, f, description)
			waitForBlockedPlaybackWrite(t, pending, done)
			if err := f.player.Stop(t.Context(), f.who, description.SessionID); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("stopped playback hid its interrupted network write")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stopping playback left its network write blocked")
			}
		})
	}
}

func TestPlaybackDisconnectInterruptsBlockedNetworkWrite(t *testing.T) {
	for _, mode := range []string{"proxy", "processed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, description, _ := backpressureFixture(t, mode)
			connection, _, pending, done := stalledPlaybackConnection(t, f, description)
			waitForBlockedPlaybackWrite(t, pending, done)
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("disconnected playback hid its interrupted network write")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("browser disconnection left its network write blocked")
			}
			// 断开后应释放消费者，当前尝试仍能由新连接接续。
			stream, err := f.player.OpenStream(t.Context(), f.who, description.SessionID, description.AttemptID, false)
			if err != nil {
				t.Fatal("disconnected browser retained its stream consumer", err)
			}
			stream.Close()
		})
	}
}

func backpressureFixture(t *testing.T, mode string) (recoveryFixture, playback.Description, []byte) {
	t.Helper()
	var processor *audioffmpeg.Processor
	if mode == "processed" {
		ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
		if ffmpeg == "" || ffprobe == "" {
			t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for processed stream backpressure validation")
		}
		var err error
		processor, err = audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, MaxConcurrent: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	var f recoveryFixture
	var payload []byte
	if mode == "processed" {
		var err error
		payload, err = os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
		if err != nil {
			t.Fatal(err)
		}
		// 生成一分钟音频，确保编码后的数据也超过操作系统的 TCP 缓冲。
		payload = shiftWave(t, payload, 54000)
		f = metadataPlaybackFixture(t, "browser-buffer.wav", payload, processor, nil)
		f.request.Preferences.DSPExecution = "server"
	} else {
		f = recoverySetup(t, processor, 0)
	}
	f.request.Preferences.ForceProcessing = mode == "processed"
	description := f.create(t, "backpressure-create")
	if description.DeliveryMode != mode {
		t.Fatalf("expected %s delivery, got %s", mode, description.DeliveryMode)
	}
	var expected []byte
	var err error
	if mode == "processed" {
		// 参考输出使用独立数据库和缓存，受测会话始终执行首次转码。
		reference := metadataPlaybackFixture(t, "reference.wav", payload, processor, nil)
		reference.request.Preferences = f.request.Preferences
		expected, err = processedBytes(t, reference, reference.create(t, "backpressure-reference"))
		if records, cacheErr := f.repo.ListProcessedCache(t.Context()); cacheErr != nil || len(records) != 0 {
			t.Fatal("backpressure fixture unexpectedly reused a processed cache", records, cacheErr)
		}
	} else {
		expected, err = os.ReadFile(f.path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return f, description, expected
}

func stalledPlaybackConnection(t *testing.T, f recoveryFixture, description playback.Description) (*net.TCPConn, *http.Response, *atomic.Bool, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	pending := &atomic.Bool{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := f.player.OpenStream(r.Context(), f.who, description.SessionID, description.AttemptID, false)
		if err == nil {
			err = streamhttp.Serve(&blockedWriteObserver{ResponseWriter: w, pending: pending}, r, stream)
		}
		done <- err
	}))
	server.Listener = smallPlaybackBufferListener{server.Listener}
	server.Start()
	t.Cleanup(server.Close)
	connection, err := net.DialTCP("tcp", nil, server.Listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	if err = connection.SetReadBuffer(8 << 10); err != nil {
		t.Fatal(err)
	}
	if err = connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintf(connection, "GET /stream HTTP/1.1\r\nHost: playback.test\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("stream did not start", response, err)
	}
	return connection, response, pending, done
}

func waitForBlockedPlaybackWrite(t *testing.T, pending *atomic.Bool, done <-chan error) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	var blockedSince time.Time
	for {
		select {
		case err := <-done:
			t.Fatalf("stream ended before network backpressure was established: %v", err)
		case <-deadline.C:
			t.Fatal("stream did not reach a blocked network write")
		case now := <-tick.C:
			if !pending.Load() {
				blockedSince = time.Time{}
			} else if blockedSince.IsZero() {
				blockedSince = now
			} else if now.Sub(blockedSince) >= 200*time.Millisecond {
				return
			}
		}
	}
}

type smallPlaybackBufferListener struct{ net.Listener }

func (l smallPlaybackBufferListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if err = connection.(*net.TCPConn).SetWriteBuffer(8 << 10); err != nil {
		connection.Close()
		return nil, err
	}
	return connection, nil
}

type blockedWriteObserver struct {
	http.ResponseWriter
	pending *atomic.Bool
}

func (w *blockedWriteObserver) Write(data []byte) (int, error) {
	w.pending.Store(true)
	defer w.pending.Store(false)
	return w.ResponseWriter.Write(data)
}

func (w *blockedWriteObserver) FlushError() error {
	w.pending.Store(true)
	defer w.pending.Store(false)
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *blockedWriteObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }

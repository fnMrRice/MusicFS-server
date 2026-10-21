package stream

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func Serve(w http.ResponseWriter, r *http.Request, stream *playback.Stream) error {
	defer stream.Close()
	controller := http.NewResponseController(w)
	// Cancellation must interrupt a blocked network write, not only file reads.
	deadlineDone := make(chan struct{})
	stopDeadline := context.AfterFunc(stream.Context, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(deadlineDone)
	})
	defer func() {
		if !stopDeadline() {
			<-deadlineDone
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	w.Header().Set("Content-Type", stream.Description.OutputProfile.MIME)
	w.Header().Set("Cache-Control", "private, no-store")
	if stream.Description.DeliveryMode == "proxy" {
		if r.Method == http.MethodGet {
			if err := ValidateRangeHeader(r.Header.Get("Range")); err != nil {
				return err
			}
		}
		w.Header().Set("ETag", stream.ETag)
		http.ServeContent(&idleWriter{ResponseWriter: w, controller: controller}, r, "audio", time.Time{}, stream.File)
		return nil
	}
	// Processed output has no byte-range, length, or source validator.
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { err := stream.Process(writer); writer.CloseWithError(err); done <- err }()
	defer func() {
		reader.Close()
		stream.Cancel()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
		}
	}()
	first := bufio.NewReaderSize(reader, 32<<10)
	// Wait for an actual encoded frame before starting HTTP output.
	started := make(chan error, 1)
	go func() { _, err := first.Peek(1024); started <- err }()
	select {
	case err := <-started:
		if err != nil {
			return domain.Fail("UNSUPPORTED", "Audio processing could not produce a playable stream")
		}
	case <-time.After(10 * time.Second):
		return domain.Fail("TEMPORARY_FAILURE", "Audio processing start timed out")
	case <-stream.Context.Done():
		return context.Canceled
	}
	w.WriteHeader(http.StatusOK)
	_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if err := controller.Flush(); err != nil {
		return err
	}
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := first.Read(buffer)
		if n > 0 {
			controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := w.Write(buffer[:n]); err != nil {
				return err
			}
			if err := controller.Flush(); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if err := stream.Context.Err(); err != nil {
			return err
		}
	}
}

type idleWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
}

func (w *idleWriter) Write(data []byte) (int, error) {
	_ = w.controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return w.ResponseWriter.Write(data)
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

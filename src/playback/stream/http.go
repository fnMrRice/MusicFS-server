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
	// 浏览器缓冲充足时会暂停读取，正常背压不能被固定写超时中断。
	// 仅在会话或请求取消时打断阻塞写入，确保停止播放仍能释放资源。
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
	output := &deliveryWriter{ResponseWriter: w, controller: controller, ctx: stream.Context, sessionID: stream.Description.SessionID}
	w = output
	w.Header().Set("Content-Type", stream.OutputMIME())
	w.Header().Set("Cache-Control", "private, no-store")
	if stream.Online {
		return serveOnline(w, r, stream, output)
	}
	if stream.Description.DeliveryMode == "proxy" {
		if r.Method == http.MethodGet {
			if err := ValidateRangeHeader(r.Header.Get("Range")); err != nil {
				return err
			}
		}
		w.Header().Set("ETag", stream.ETag)
		http.ServeContent(w, r, "audio", time.Time{}, stream.File)
		return output.err
	}
	// 处理后的流不提供字节范围、长度或源文件校验标识。
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
	// 首个编码帧就绪后再发送 HTTP 响应。
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
	if err := output.FlushError(); err != nil {
		return err
	}
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := first.Read(buffer)
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				return err
			}
			if err := output.FlushError(); err != nil {
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

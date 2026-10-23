package stream

import (
	"context"
	"io"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func serveOnline(w http.ResponseWriter, r *http.Request, stream *playback.Stream, output *deliveryWriter) error {
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	// 浏览器探测首字节的请求仍返回完整顺序流，不承诺任何跨请求 Range 身份。
	if value := r.Header.Get("Range"); value != "" && value != "bytes=0-" {
		return domain.Fail("RESTART_REQUIRED", "Online audio does not support byte ranges")
	}
	if r.Header.Get("If-Range") != "" {
		return domain.Fail("RESTART_REQUIRED", "Online audio does not support byte resumption")
	}
	body, err := stream.OpenOnline()
	if err != nil {
		return err
	}
	defer body.Close()
	stop := context.AfterFunc(stream.Context, func() { body.Close() })
	defer stop()
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := body.Read(buffer)
		if n > 0 {
			if _, err = w.Write(buffer[:n]); err != nil {
				return err
			}
			if err = output.FlushError(); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
		if err = stream.Context.Err(); err != nil {
			return err
		}
	}
}

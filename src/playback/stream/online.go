package stream

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func serveOnline(w http.ResponseWriter, r *http.Request, stream *playback.Stream, output *deliveryWriter) error {
	if stream.OpenOnlineRange != nil {
		if err := ValidateRangeHeader(r.Header.Get("Range")); err != nil {
			return err
		}
		w.Header().Set("ETag", stream.ETag)
		// 只用已知长度和验证器处理没有正文的探测；无需为 304、412 或 416 下载音频。
		empty := func() io.ReadSeeker { return io.NewSectionReader(bytes.NewReader(nil), 0, stream.Size) }
		var audio io.ReadSeeker = empty()
		if r.Method != http.MethodHead {
			if r.Header.Get("Range") != "" || r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Match") != "" {
				probe := *r
				probe.Method = http.MethodHead
				result := rangeProbe{header: w.Header().Clone()}
				http.ServeContent(&result, &probe, "audio", time.Time{}, empty())
				if result.status != http.StatusOK && result.status != http.StatusPartialContent {
					http.ServeContent(w, r, "audio", time.Time{}, empty())
					return output.err
				}
			}
			var err error
			audio, err = stream.OpenOnlineRange()
			if err != nil {
				return err
			}
			w.Header().Set("Content-Type", stream.OutputMIME())
		}
		http.ServeContent(w, r, "audio", time.Time{}, audio)
		return output.err
	}
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
	w.Header().Set("Content-Type", stream.OutputMIME())
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

type rangeProbe struct {
	header http.Header
	status int
}

func (p *rangeProbe) Header() http.Header {
	return p.header
}
func (p *rangeProbe) WriteHeader(code int)           { p.status = code }
func (p *rangeProbe) Write(data []byte) (int, error) { return len(data), nil }

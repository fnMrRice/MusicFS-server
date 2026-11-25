package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

var controlRoute = regexp.MustCompile(`^(POST /playback/sessions|DELETE /playback/sessions/[A-Za-z0-9_-]{1,128}|POST /playback/sessions/[A-Za-z0-9_-]{1,128}/(seek|recover|recover-source)|GET /lifecycle|POST /lifecycle/leases|POST /lifecycle/leases/[A-Za-z0-9_-]{1,128}/(renew|release))$`)

type controlDispatchKey struct{}

func (c *controlConnection) failure(id string, err error) controlResponse {
	w := newControlResult()
	r := c.request.WithContext(context.WithValue(c.ctx, requestIDKey{}, id))
	WriteError(w, r, err)
	return controlResponse{Type: "response", ID: id, Status: w.status, Body: w.body.Bytes()}
}

func (c *controlConnection) dispatch(message controlRequest) (result controlResponse) {
	defer func() {
		if recover() != nil {
			result = c.failure(message.ID, domain.Fail("TEMPORARY_FAILURE", "Control request could not be completed"))
		}
	}()
	if len(message.Path) > 512 || len(message.IdempotencyKey) > 128 || !controlRoute.MatchString(message.Method+" "+message.Path) {
		return c.failure(message.ID, domain.Fail("INVALID_ARGUMENT", "Control route or idempotency key is invalid"))
	}
	// 仅允许明确列出的控制路由，复用 HTTP 的鉴权、所有权与业务参数校验。
	ctx, cancel := context.WithDeadline(c.ctx, message.deadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return c.failure(message.ID, err)
	}
	r := c.request.Clone(context.WithValue(ctx, controlDispatchKey{}, true))
	r.Method = message.Method
	r.URL = &url.URL{Path: "/api/v1" + message.Path}
	r.RequestURI = r.URL.Path
	r.Header = http.Header{}
	r.Header.Set("Cookie", c.request.Header.Get("Cookie"))
	r.Header.Set("Origin", c.request.Header.Get("Origin"))
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.Header.Set("X-Request-ID", message.ID)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", message.IdempotencyKey)
	r.Body = io.NopCloser(bytes.NewReader(message.Body))
	r.ContentLength = int64(len(message.Body))
	w := newControlResult()
	c.server.ServeHTTP(w, r)
	if w.overflow || w.body.Len() > 0 && !json.Valid(w.body.Bytes()) {
		return c.failure(message.ID, domain.Fail("TEMPORARY_FAILURE", "Control response exceeds its limit"))
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return controlResponse{Type: "response", ID: message.ID, Status: w.status, Body: w.body.Bytes()}
}

type controlResult struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func newControlResult() *controlResult       { return &controlResult{header: http.Header{}} }
func (w *controlResult) Header() http.Header { return w.header }
func (w *controlResult) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *controlResult) Write(body []byte) (int, error) {
	if w.body.Len()+len(body) > 1<<20 {
		w.overflow = true
		return 0, errors.New("control response exceeds its limit")
	}
	w.WriteHeader(http.StatusOK)
	return w.body.Write(body)
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type controlRequest struct {
	deadline       time.Time
	Type           string          `json:"type"`
	ID             string          `json:"id"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	Body           json.RawMessage `json:"body,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
}

type controlResponse struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
}

func controlDecode(data []byte, value any) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value) == nil && decoder.Decode(new(any)) == io.EOF
}

func (c *controlConnection) readLoop() {
	deadline := time.Now().Add(controlHeartbeatTimeout)
	for {
		ctx, cancel := context.WithDeadline(c.ctx, deadline)
		kind, data, err := c.conn.Read(ctx)
		cancel()
		if err != nil {
			return
		}
		receivedAt := time.Now()
		deadline = receivedAt.Add(controlHeartbeatTimeout)
		var header struct {
			Type string `json:"type"`
		}
		if kind != websocket.MessageText || json.Unmarshal(data, &header) != nil || !c.authorized() {
			_ = c.conn.Close(websocket.StatusPolicyViolation, "Invalid control message")
			return
		}
		switch header.Type {
		case "ping":
			if !controlDecode(data, &header) {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "Invalid heartbeat")
				return
			}
			c.send(struct {
				Type string `json:"type"`
			}{"pong"})
		case "request":
			var request controlRequest
			if !controlDecode(data, &request) || !safeRequestID.MatchString(request.ID) {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "Invalid control request")
				return
			}
			request.deadline = receivedAt.Add(c.server.options.ControlTimeout)
			c.mu.Lock()
			duplicate := c.pending[request.ID]
			c.pending[request.ID] = true
			c.mu.Unlock()
			if duplicate {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "Duplicate pending request ID")
				return
			}
			if request.Path == "/tasks/subscription" {
				c.configureTaskSubscription(request)
				continue
			}
			queue := c.jobs
			if strings.HasPrefix(request.Path, "/lifecycle") {
				// 生命周期独立排队，播放准备耗时不能饿死租约续租。
				queue = c.leaseJobs
			}
			select {
			case queue <- request:
			default:
				c.send(c.failure(request.ID, domain.Fail("RATE_LIMITED", "Control request queue is full")))
			}
		default:
			_ = c.conn.Close(websocket.StatusPolicyViolation, "Unknown control message")
			return
		}
	}
}

func (c *controlConnection) send(message any) {
	select {
	case <-c.ctx.Done():
	case c.out <- message:
	default:
		// 队列饱和即断开，客户端重连后重新获取快照，不无限积压。
		c.cancel()
	}
}

func (c *controlConnection) writeLoop() {
	defer c.cancel()
	for {
		select {
		case <-c.ctx.Done():
			return
		case message := <-c.out:
			ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
			err := wsjson.Write(ctx, c.conn, message)
			cancel()
			if err != nil {
				return
			}
			if response, ok := message.(controlResponse); ok {
				c.mu.Lock()
				delete(c.pending, response.ID)
				c.mu.Unlock()
			}
		}
	}
}

func (c *controlConnection) commandLoop(jobs <-chan controlRequest) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case request := <-jobs:
			if c.ctx.Err() != nil {
				return
			}
			c.send(c.dispatch(request))
		}
	}
}

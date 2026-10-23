package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

const controlProtocol = "musicfs.control.v1"
const controlHeartbeatTimeout = 30 * time.Second

type controlHub struct {
	mu          sync.Mutex
	wait        sync.WaitGroup
	closing     bool
	connections map[*controlConnection]string
	plugins     *plugincontrol.Manager
	lifecycle   *lifecycle.Manager
}

type controlConnection struct {
	server    *Server
	conn      *websocket.Conn
	request   *http.Request
	session   auth.Session
	csrf      string
	ctx       context.Context
	cancel    context.CancelFunc
	out       chan any
	jobs      chan controlRequest
	leaseJobs chan controlRequest
	mu        sync.Mutex
	pending   map[string]bool
}

// CloseControl 取消控制命令并主动关闭被 HTTP 服务接管的长连接。
func (s *Server) CloseControl() {
	s.control.mu.Lock()
	s.control.closing = true
	connections := make([]*websocket.Conn, 0, len(s.control.connections))
	for c := range s.control.connections {
		c.cancel()
		if c.conn != nil {
			connections = append(connections, c.conn)
		}
	}
	s.control.mu.Unlock()
	// 关闭网络连接时不持有全局锁，慢客户端不会阻塞其他会话。
	for _, conn := range connections {
		_ = conn.CloseNow()
	}
}

// WaitControl 等待取消后的控制命令退出；调用方必须先调用 CloseControl。
func (s *Server) WaitControl() { s.control.wait.Wait() }

func (s *Server) openControl(w http.ResponseWriter, r *http.Request, session auth.Session) error {
	if r.URL.RawQuery != "" || !s.options.Auth.OriginAllowed(r.Header.Get("Origin")) {
		return domain.Fail("ACCESS_DENIED", "Control origin or URL is not allowed")
	}
	if !slices.ContainsFunc(strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ","), func(value string) bool { return strings.TrimSpace(value) == controlProtocol }) {
		return domain.Fail("INVALID_ARGUMENT", "Control subprotocol is required")
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c := &controlConnection{server: s, request: r, session: session, ctx: ctx, cancel: cancel,
		out: make(chan any, 32), jobs: make(chan controlRequest, 8), leaseJobs: make(chan controlRequest, 8), pending: map[string]bool{}}
	s.control.mu.Lock()
	count := 0
	for _, id := range s.control.connections {
		if id == session.ID {
			count++
		}
	}
	if s.control.closing || count >= 8 {
		s.control.mu.Unlock()
		return domain.Fail("RATE_LIMITED", "Control connections are unavailable")
	}
	s.control.connections[c] = session.ID
	s.control.wait.Add(1)
	s.control.mu.Unlock()
	defer func() {
		s.control.mu.Lock()
		delete(s.control.connections, c)
		s.control.mu.Unlock()
		s.control.wait.Done()
	}()
	// Origin 已由部署配置精确验证，允许配置中的跨主机代理入口。
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{controlProtocol}, InsecureSkipVerify: true})
	if err != nil {
		return nil
	}
	defer conn.CloseNow()
	s.control.mu.Lock()
	c.conn = conn
	closing := s.control.closing
	s.control.mu.Unlock()
	if closing {
		return nil
	}
	conn.SetReadLimit(MaxRequestBytes)
	if !c.authenticate() {
		return nil
	}
	c.send(struct {
		Type                string `json:"type"`
		InstanceID          string `json:"instanceId"`
		HeartbeatIntervalMs int    `json:"heartbeatIntervalMs"`
		HeartbeatTimeoutMs  int    `json:"heartbeatTimeoutMs"`
	}{"ready", s.options.InstanceID, 10000, 30000})
	var workers sync.WaitGroup
	workers.Go(c.writeLoop)
	workers.Go(func() { c.commandLoop(c.jobs) })
	workers.Go(func() { c.commandLoop(c.leaseJobs) })
	workers.Go(c.eventLoop)
	c.readLoop()
	c.cancel()
	_ = conn.CloseNow()
	workers.Wait()
	return nil
}

func (c *controlConnection) authenticate() bool {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	kind, data, err := c.conn.Read(ctx)
	var message struct {
		Type      string `json:"type"`
		CSRFToken string `json:"csrfToken"`
	}
	if err != nil {
		return false
	}
	if kind != websocket.MessageText || !controlDecode(data, &message) || message.Type != "auth" ||
		c.server.options.Auth.CheckMutation(c.session, c.request.Header.Get("Origin"), message.CSRFToken) != nil {
		_ = c.conn.Close(websocket.StatusPolicyViolation, "Invalid control authentication")
		return false
	}
	c.csrf = message.CSRFToken
	return c.authorized()
}

func (c *controlConnection) authorized() bool {
	ctx, cancel := context.WithTimeout(c.ctx, c.server.options.ControlTimeout)
	defer cancel()
	cookie, err := c.request.Cookie(CookieName)
	if err != nil {
		return false
	}
	session, err := c.server.options.Auth.Authenticate(ctx, cookie.Value)
	if err != nil || session.ID != c.session.ID || c.server.options.Auth.CheckMutation(session, c.request.Header.Get("Origin"), c.csrf) != nil {
		_ = c.conn.Close(websocket.StatusPolicyViolation, "Authentication required")
		return false
	}
	return true
}

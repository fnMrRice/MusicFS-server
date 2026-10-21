package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type User struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}
type Session struct {
	ID        string    `json:"-"`
	User      User      `json:"user"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type Repository interface {
	SaveAuth(context.Context, Session) error
	FindAuth(context.Context, string) (Session, error)
	DeleteAuth(context.Context, string) error
}
type attempt struct {
	count int
	since time.Time
}
type Service struct {
	repo      Repository
	tokenHash [32]byte
	origins   map[string]bool
	ttl       time.Duration
	mu        sync.Mutex
	failures  map[string]attempt
	OnRevoke  func(string)
}

func New(repo Repository, token string, origins []string, ttl time.Duration) *Service {
	s := &Service{repo: repo, tokenHash: sha256.Sum256([]byte(token)), origins: map[string]bool{}, ttl: ttl, failures: map[string]attempt{}}
	for _, o := range origins {
		s.origins[o] = true
	}
	return s
}
func Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) OriginAllowed(origin string) bool { return s.origins[origin] }
func (s *Service) Login(ctx context.Context, credential, origin, remote string) (Session, string, error) {
	if !s.OriginAllowed(origin) {
		return Session{}, "", domain.Fail("ACCESS_DENIED", "Origin is not allowed")
	}
	s.mu.Lock()
	for k, v := range s.failures {
		if time.Since(v.since) > time.Minute {
			delete(s.failures, k)
		}
	}
	a := s.failures[remote]
	if a.count >= 5 || len(s.failures) > 1024 {
		s.mu.Unlock()
		return Session{}, "", domain.Fail("RATE_LIMITED", "Too many login attempts")
	}
	provided := sha256.Sum256([]byte(credential))
	valid := subtle.ConstantTimeCompare(provided[:], s.tokenHash[:]) == 1
	if !valid {
		if a.count == 0 {
			a.since = time.Now()
		}
		a.count++
		s.failures[remote] = a
		s.mu.Unlock()
		return Session{}, "", domain.Fail("AUTH_REQUIRED", "Invalid credential")
	}
	delete(s.failures, remote)
	s.mu.Unlock()
	value := domain.NewID()
	session := Session{ID: Digest(value), User: User{ID: "admin", Role: "admin"}, CSRFToken: domain.NewID(), ExpiresAt: time.Now().UTC().Add(s.ttl)}
	if err := s.repo.SaveAuth(ctx, session); err != nil {
		return Session{}, "", err
	}
	return session, value, nil
}
func (s *Service) Authenticate(ctx context.Context, cookie string) (Session, error) {
	if len(cookie) != 48 {
		return Session{}, domain.Fail("AUTH_REQUIRED", "Login is required")
	}
	session, err := s.repo.FindAuth(ctx, Digest(cookie))
	if err != nil || time.Now().After(session.ExpiresAt) {
		return Session{}, domain.Fail("AUTH_REQUIRED", "Session expired or revoked")
	}
	return session, nil
}
func (s *Service) CheckMutation(session Session, origin, csrf string) error {
	if !s.OriginAllowed(origin) || subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRFToken)) != 1 {
		return domain.Fail("ACCESS_DENIED", "Origin or CSRF token is invalid")
	}
	return nil
}
func (s *Service) Revoke(ctx context.Context, id string) error {
	if err := s.repo.DeleteAuth(ctx, id); err != nil {
		return err
	}
	if s.OnRevoke != nil {
		s.OnRevoke(id)
	}
	return nil
}

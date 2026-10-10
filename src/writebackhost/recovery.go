package writebackhost

import (
	"context"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

func (s *Service) recoverStartup(ctx context.Context) error {
	pending := []writeback.Record{}
	ids := map[string]bool{}
	after := ""
	for {
		page, err := s.journal.Pending(ctx, after, 128)
		if err != nil {
			return err
		}
		for _, r := range page {
			ids[r.ID] = true
			pending = append(pending, r)
			_ = s.lib.BlockProvider(r.Owner, r.Target.InstanceID, r.ID)
		}
		if len(pending) > 1024 {
			return domain.Fail("RATE_LIMITED", "Too many unresolved writeback operations")
		}
		if len(page) < 128 {
			break
		}
		after = page[len(page)-1].ID
	}
	if err := s.repo.RecoverWritebackQueue(ctx, ids); err != nil {
		return err
	}
	for _, r := range pending {
		recoveryCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		job, err := s.repo.GetWriteback(recoveryCtx, r.Owner, r.ID)
		if err == nil && job.Request.Overwrite && !s.cfg.AllowOverwrite {
			err = domain.Fail("ACCESS_DENIED", "Overwriting source tags is disabled")
		}
		if err == nil {
			_, err = s.editor.Recover(recoveryCtx, r.Owner, r.ID)
		}
		cancel()
		s.settle(r.Owner, r.ID, r.Target, err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (s *Service) Recover(ctx context.Context, owner, id string) (Job, error) {
	j, err := s.repo.GetWriteback(ctx, owner, id)
	if err != nil {
		return j, err
	}
	if j.Status == "succeeded" {
		r, loadErr := s.journal.Load(ctx, id)
		if loadErr != nil {
			return j, loadErr
		}
		if r.Stage == "committed" {
			return s.Get(ctx, owner, id)
		}
	}
	if j.Status != "recovery" && j.Status != "succeeded" {
		return j, domain.Fail("CONFLICT", "Writeback job is not awaiting recovery")
	}
	if !s.cfg.Policy.Enabled {
		return j, domain.Fail("ACCESS_DENIED", "Source writeback is disabled")
	}
	if j.Request.Overwrite && !s.cfg.AllowOverwrite {
		return j, domain.Fail("ACCESS_DENIED", "Overwriting source tags is disabled")
	}
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if len(s.retries) >= 128 && s.retries[id] == "" {
		return j, domain.Fail("RATE_LIMITED", "Writeback recovery queue is full")
	}
	s.retries[id] = owner
	return s.Get(ctx, owner, id)
}

// Explicit recovery is queued so large files cannot hold a control request.
// The existing durable journal is its intent; startup also resumes that intent.
func (s *Service) runRecovery() bool {
	s.recoveryMu.Lock()
	var id, owner string
	for key, value := range s.retries {
		id, owner = key, value
		break
	}
	s.recoveryMu.Unlock()
	if id == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	w, err := s.repo.WritebackWork(ctx, owner, id)
	if err == nil {
		_, err = s.editor.Recover(ctx, owner, id)
	}
	if errors.Is(err, errBusy) {
		return true
	}
	if w.Job.ID != "" {
		s.settle(owner, id, w.Target, err)
	}
	s.recoveryMu.Lock()
	delete(s.retries, id)
	s.recoveryMu.Unlock()
	return true
}

func (s *Service) settle(owner, id string, target writeback.Target, operationErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := s.journal.Load(ctx, id)
	code := "INTERNAL"
	var de *domain.Error
	if errors.As(operationErr, &de) {
		code = de.Code
	} else if errors.Is(operationErr, context.Canceled) {
		code = "CANCELED"
	} else if errors.Is(operationErr, context.DeadlineExceeded) {
		code = "TIMEOUT"
	}
	if err == nil && r.Stage == "committed" {
		return
	}
	if err == nil && r.Stage == "aborted" {
		_ = s.repo.SetWritebackState(ctx, owner, id, "failed", code)
		s.clearBarrier(ctx, owner, id, target)
		return
	}
	if errors.As(err, &de) && de.Code == "NOT_FOUND" {
		_ = s.repo.SetWritebackState(ctx, owner, id, "failed", code)
		s.clearBarrier(ctx, owner, id, target)
		return
	}
	_ = s.lib.BlockProvider(owner, target.InstanceID, id)
	_ = s.repo.SetWritebackState(ctx, owner, id, "recovery", code)
}

func (s *Service) clearBarrier(ctx context.Context, owner, id string, target writeback.Target) {
	l, err := s.lib.TryExclusive(ctx, owner, target.InstanceID, id)
	if err == nil {
		l.ClearBarrier()
		l.Close()
	}
}

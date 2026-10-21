package hashworker

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

type limiter struct {
	mu   sync.Mutex
	rate int64
	next time.Time
}

func (l *limiter) wait(ctx context.Context, n int) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	l.next = l.next.Add(time.Duration(int64(n) * int64(time.Second) / l.rate))
	delay := time.Until(l.next)
	l.mu.Unlock()
	if !pause(ctx, delay) {
		return ctx.Err()
	}
	return nil
}

func compute(ctx context.Context, in identification.StableInput, size int64, before func(context.Context, int) error, progress func(int64) error) (string, int64, error) {
	if size < 0 {
		return "", 0, identification.Invalid()
	}
	if err := in.Validate(); err != nil {
		return "", 0, err
	}
	h := sha512.New()
	buf := make([]byte, 64<<10)
	var offset int64
	for offset < size {
		if err := ctx.Err(); err != nil {
			return "", offset, err
		}
		want := int64(len(buf))
		if size-offset < want {
			want = size - offset
		}
		if err := before(ctx, int(want)); err != nil {
			return "", offset, err
		}
		n, err := in.ReadAt(buf[:want], offset)
		if n != int(want) || err != nil {
			return "", offset, domain.Fail("RESOURCE_CHANGED", "Complete original file could not be read")
		}
		_, _ = h.Write(buf[:n])
		offset += int64(n)
		if err := progress(offset); err != nil {
			return "", offset, err
		}
	}
	// A trusted ReaderAt must reject reads beyond the observed complete size.
	var extra [1]byte
	n, err := in.ReadAt(extra[:], size)
	if n != 0 || !errors.Is(err, io.EOF) {
		return "", offset, domain.Fail("RESOURCE_CHANGED", "Original file size changed")
	}
	if err := ctx.Err(); err != nil {
		return "", offset, err
	}
	if err := in.Validate(); err != nil {
		return "", offset, err
	}
	return hex.EncodeToString(h.Sum(nil)), offset, nil
}

func failure(err error) identification.Result {
	code := pluginproto.ErrorCode(err)
	var known *domain.Error
	if errors.As(err, &known) {
		code = known.Code
	}
	if errors.Is(err, context.Canceled) {
		code = "CANCELED"
	}
	state := "failed"
	if code == "UNSUPPORTED" || code == "ACCESS_DENIED" || code == "SOURCE_OFFLINE" {
		state = "unavailable"
	}
	return identification.Result{Status: state, BitrateMode: "unknown", FailureCode: &code}
}

func (s *Service) execute(parent context.Context, j identification.HashJob) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.running[j.ID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, j.ID); s.mu.Unlock() }()
	current, err := s.repo.GetHashJob(ctx, j.Owner, j.ID)
	if err != nil || current.Status != "running" || current.Generation != j.Generation {
		return
	}
	in, err := s.source.OpenStableHash(ctx, j.Owner, j.ID, j.HashTarget)
	var completed int64
	var result identification.Result
	if err == nil {
		// Keep the read guard alive across the publication transaction.
		defer in.Close()
		last := time.Now()
		var digest string
		digest, completed, err = compute(ctx, in, j.FileSizeBytes, s.pace.wait, func(n int64) error {
			if time.Since(last) < time.Second {
				return nil
			}
			last = time.Now()
			return s.repo.HashProgress(ctx, j, n)
		})
		if err == nil {
			now := time.Now().UTC()
			result = identification.Result{Status: "verified", BitrateMode: "unknown", SHA512: &digest, ComputedAt: &now}
		}
	}
	// Shutdown leaves a running task durable for recovery. User cancellation
	// has already changed its state atomically and cannot be overwritten here.
	if parent.Err() != nil {
		return
	}
	if err != nil {
		result = failure(err)
	}
	if err := s.finish(j, result, completed, in); err != nil {
		slog.Error("Hash task publication failed; durable task requires recovery", "jobId", j.ID, "errorCode", pluginproto.ErrorCode(err))
	}
}

func (s *Service) finish(j identification.HashJob, result identification.Result, completed int64, in identification.StableInput) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if result.Status == "verified" {
			err = in.Publish(func() error { return s.repo.FinishHash(ctx, j, result, completed) })
		} else {
			err = s.repo.FinishHash(ctx, j, result, completed)
		}
		if err == nil {
			return nil
		}
		current, getErr := s.repo.GetHashJob(ctx, j.Owner, j.ID)
		if getErr == nil && (current.Status != "running" || current.Generation != j.Generation) {
			return nil
		}
		// A rejected protected publish can only become a failure, never a
		// verified retry after the read protection has been broken.
		result = failure(err)
		if !pause(ctx, 50*time.Millisecond) {
			break
		}
	}
	return err
}

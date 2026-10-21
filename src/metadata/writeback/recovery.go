package writeback

import (
	"context"
	"errors"
	"os"
	"path"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func (s *Service) Recover(ctx context.Context, owner, id string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !jobID.MatchString(id) {
		return Result{}, invalid("Invalid writeback job ID")
	}
	record, err := s.journal.Load(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if record.Owner != owner {
		return Result{}, domain.Fail("NOT_FOUND", "Writeback job not found")
	}
	return s.recover(ctx, record)
}

func (s *Service) recover(ctx context.Context, record Record) (result Result, err error) {
	root, err := s.authorize(record.Owner, record.Target, record.Fields)
	if err != nil {
		return result, err
	}
	if root.Identity != record.RootIdentity {
		return result, changed()
	}
	if record.Stage == "committed" {
		return resultOf(record), nil
	}
	if record.Stage == "aborted" || record.Stage == "blocked" {
		return result, domain.Fail("CONFLICT", "Writeback requires a new operation or manual recovery")
	}
	lease, err := s.coordinator.Recover(ctx, record)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	if lifetime, ok := lease.(interface{ Context() context.Context }); ok {
		ctx = lifetime.Context()
	}
	if err = lease.Check(ctx); err != nil {
		return result, err
	}
	fs, err := openDisk(root, record.Target)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	read := func(name string) (*os.File, FileState, error) {
		f, e := fs.Open(name)
		if errors.Is(e, os.ErrNotExist) {
			return nil, FileState{}, nil
		}
		if e != nil {
			return nil, FileState{}, e
		}
		state, e := s.describe(ctx, fs, f)
		if e != nil {
			f.Close()
			return nil, state, e
		}
		return f, state, nil
	}
	original, current, err := read(path.Base(record.Target.RelativePath))
	if err != nil {
		return result, err
	}
	if original != nil {
		defer func() { err = errors.Join(err, original.Close()) }()
	}
	// Before written, no path mutation was authorized. Partial staging bytes are
	// retained for inspection; recovery never guesses their intended contents.
	if record.Stage == "prepared" {
		if original == nil || current != record.Old {
			return result, s.block(ctx, &record)
		}
		if err = s.save(ctx, &record, "aborted"); err != nil {
			return result, err
		}
		return result, domain.Fail("CONFLICT", "Incomplete writeback was safely aborted; use a new job ID")
	}
	if !record.Changed {
		if original == nil || current != record.Old {
			return result, s.block(ctx, &record)
		}
		staging, pending, readErr := read(stageName(record.ID))
		if readErr != nil {
			return result, readErr
		}
		if staging != nil {
			defer func() { err = errors.Join(err, staging.Close()) }()
			if pending != record.New {
				return result, s.block(ctx, &record)
			}
			if err = fs.Discard(staging); err != nil {
				return result, err
			}
		}
		record.New = record.Old
		return s.commit(ctx, lease, &record)
	}
	backup, oldBackup, err := read(backupName(record.ID))
	if err != nil {
		return result, err
	}
	if backup != nil {
		defer func() { err = errors.Join(err, backup.Close()) }()
	}
	// Matching full bytes and file identity is required after an interrupted
	// rename. A rename changes NTFS change time, so its pre-rename version alone
	// cannot decide the ambiguous window between the file and journal updates.
	if original != nil && sameBytes(current, record.New) && backup != nil && sameBytes(oldBackup, record.Old) {
		if record.Stage == "installed" && current.Version != record.New.Version {
			return result, s.block(ctx, &record)
		}
		if err = s.verifyAudio(ctx, original, current.Size, record.AudioSHA512); err != nil {
			return result, s.block(ctx, &record)
		}
		record.New = current
		if record.Stage != "installed" {
			if err = s.save(ctx, &record, "installed"); err != nil {
				return result, err
			}
		}
		return s.commit(ctx, lease, &record)
	}
	temp, pending, err := read(stageName(record.ID))
	if err != nil {
		return result, err
	}
	if temp != nil {
		defer func() { err = errors.Join(err, temp.Close()) }()
	}
	if temp == nil || pending != record.New {
		return result, s.block(ctx, &record)
	}
	if err = s.verifyAudio(ctx, temp, pending.Size, record.AudioSHA512); err != nil {
		return result, s.block(ctx, &record)
	}
	if original != nil {
		if current != record.Old || backup != nil || record.Stage != "written" {
			return result, s.block(ctx, &record)
		}
		if err = lease.Check(ctx); err != nil {
			return result, err
		}
		if err = fs.Rename(original, backupName(record.ID)); err != nil {
			return result, err
		}
		if err = s.save(ctx, &record, "displaced"); err != nil {
			return result, err
		}
	} else {
		if backup == nil || !sameBytes(oldBackup, record.Old) {
			return result, s.block(ctx, &record)
		}
		if record.Stage != "displaced" {
			if err = s.save(ctx, &record, "displaced"); err != nil {
				return result, err
			}
		}
	}
	return s.install(ctx, fs, lease, temp, &record)
}

func sameBytes(a, b FileState) bool {
	return a.Identity == b.Identity && a.Size == b.Size && a.SHA512 != "" && a.SHA512 == b.SHA512
}
func (s *Service) block(ctx context.Context, record *Record) error {
	return errors.Join(changed(), s.save(ctx, record, "blocked"))
}

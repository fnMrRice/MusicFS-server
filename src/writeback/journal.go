package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// FileJournal is a single-server durable journal for standalone integrations.
// Its directory is private service state, never a user-supplied music folder.
// The host must exclude a second server process, as it does for its database.
type FileJournal struct {
	root *os.Root
	mu   sync.Mutex
}

func OpenJournal(directory string) (*FileJournal, error) {
	if !filepath.IsAbs(directory) {
		return nil, invalid("Journal directory must be absolute")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	return &FileJournal{root: r}, nil
}
func (j *FileJournal) Close() error { return j.root.Close() }
func (j *FileJournal) Create(ctx context.Context, r Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !jobID.MatchString(r.ID) || r.Stage != "prepared" {
		return invalid("Invalid writeback journal intent")
	}
	_, err := j.load(ctx, r.ID)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "NOT_FOUND" {
		if err == nil {
			return domain.Fail("CONFLICT", "Writeback job already exists")
		}
		return err
	}
	return j.write(ctx, r, false)
}
func (j *FileJournal) Load(ctx context.Context, id string) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.load(ctx, id)
}
func (j *FileJournal) load(ctx context.Context, id string) (Record, error) {
	var r Record
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if !jobID.MatchString(id) {
		return r, invalid("Invalid writeback journal ID")
	}
	f, err := j.root.Open(id + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return r, domain.Fail("NOT_FOUND", "Writeback job not found")
	}
	if err != nil {
		return r, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(raw) > 65536 {
		return r, domain.Fail("TEMPORARY_FAILURE", "Writeback journal cannot be read")
	}
	if err = json.Unmarshal(raw, &r); err != nil || r.ID != id {
		return r, domain.Fail("TEMPORARY_FAILURE", "Invalid writeback journal")
	}
	return r, nil
}
func (j *FileJournal) Save(ctx context.Context, expected string, r Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	old, err := j.load(ctx, r.ID)
	if err != nil {
		return err
	}
	if old.Stage != expected || old.Owner != r.Owner || old.RequestDigest != r.RequestDigest || old.Target != r.Target || old.SelectionRevision != r.SelectionRevision || old.RootIdentity != r.RootIdentity {
		return domain.Fail("CONFLICT", "Writeback journal changed")
	}
	if !validTransition(expected, r.Stage) {
		return invalid("Invalid writeback stage transition")
	}
	return j.write(ctx, r, true)
}
func validTransition(from, to string) bool {
	if to == "blocked" {
		return from == "prepared" || from == "written" || from == "displaced" || from == "installed"
	}
	switch from {
	case "prepared":
		return to == "written" || to == "aborted"
	case "written":
		return to == "displaced" || to == "installed" || to == "committed"
	case "displaced":
		return to == "installed"
	case "installed":
		return to == "committed"
	}
	return false
}
func (j *FileJournal) write(ctx context.Context, r Record, replace bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 65536 {
		return invalid("Writeback journal exceeds its limit")
	}
	name := r.ID + "." + domain.NewID() + ".pending"
	f, err := j.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return publishJournal(j.root, name, r.ID+".json", replace)
}

package writeback

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/tagformat"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Service struct {
	policy      Policy
	journal     Journal
	coordinator Coordinator
	format      tagformat.Format
	mu          sync.Mutex
}

type disk interface {
	Open(string) (*os.File, error)
	Create(string, *os.File) (*os.File, error)
	Rename(*os.File, string) error
	Discard(*os.File) error
	State(*os.File) (FileState, error)
	Close() error
}

var jobID = regexp.MustCompile(`^[0-9a-f]{48}$`)

func invalid(message string) error     { return domain.Fail("INVALID_ARGUMENT", message) }
func unsupported(message string) error { return domain.Fail("UNSUPPORTED", message) }
func changed() error {
	return domain.Fail("RESOURCE_CHANGED", "Writeback source or recovery files changed")
}

func New(policy Policy, journal Journal, coordinator Coordinator, format tagformat.Format) (*Service, error) {
	if journal == nil || coordinator == nil || format == nil {
		return nil, invalid("Writeback requires durable journaling, host coordination and a tag format plugin")
	}
	if policy.MaxFileBytes == 0 {
		policy.MaxFileBytes = 8 << 30
	}
	if policy.MaxFileBytes < 46 || policy.MaxFileBytes > 1<<40 {
		return nil, invalid("Invalid writeback size limit")
	}
	policy.Roots = append([]Root(nil), policy.Roots...)
	seen := map[string]bool{}
	for i, root := range policy.Roots {
		key := root.Owner + "\x00" + root.InstanceID + "\x00" + root.RootID
		if root.Owner == "" || root.InstanceID == "" || root.RootID == "" || root.Identity == "" || !filepath.IsAbs(root.Path) || seen[key] {
			return nil, invalid("Invalid writeback root grant")
		}
		seen[key] = true
		root.Fields = append([]string(nil), root.Fields...)
		for _, field := range root.Fields {
			if !supportedField(field) {
				return nil, invalid("Unsupported writeback field grant")
			}
		}
		policy.Roots[i] = root
	}
	return &Service{policy: policy, journal: journal, coordinator: coordinator, format: format}, nil
}

func supportedField(field string) bool {
	switch field {
	case "title", "artists", "album", "lyrics", "artwork":
		return true
	}
	return false
}

func (s *Service) authorize(owner string, target Target, fields []string) (Root, error) {
	if !s.policy.Enabled {
		return Root{}, domain.Fail("ACCESS_DENIED", "Source writeback is disabled")
	}
	if target.SourceTrackID == "" || target.AssetID == "" || target.AssetVersion == "" || target.FileIdentity == "" || !validRelative(target.RelativePath) {
		return Root{}, invalid("Invalid writeback target binding")
	}
	for _, root := range s.policy.Roots {
		if root.Owner != owner || root.InstanceID != target.InstanceID || root.RootID != target.RootID {
			continue
		}
		allowed := map[string]bool{}
		for _, field := range root.Fields {
			allowed[field] = true
		}
		for _, field := range fields {
			if !allowed[field] {
				return Root{}, domain.Fail("ACCESS_DENIED", "Writeback field is not authorized")
			}
		}
		return root, nil
	}
	return Root{}, domain.Fail("ACCESS_DENIED", "Writeback root is not authorized")
}

func validRelative(value string) bool {
	if value == "" || !filepath.IsLocal(value) || path.Clean(value) != value || strings.ContainsAny(value, "\\:\x00") {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return false
		}
	}
	return strings.EqualFold(path.Ext(value), ".flac")
}

func (s *Service) Apply(ctx context.Context, owner string, request Request) (result Result, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !jobID.MatchString(request.ID) || request.SelectionRevision < 0 {
		return result, invalid("Invalid writeback job or revision")
	}
	if err = s.format.ValidatePatch(request.Patch); err != nil {
		return result, err
	}
	fields := []string{}
	for field := range request.Patch.Comments {
		fields = append(fields, field)
	}
	if request.Patch.Artwork != nil {
		fields = append(fields, "artwork")
	}
	sort.Strings(fields)
	root, err := s.authorize(owner, request.Target, fields)
	if err != nil {
		return result, err
	}
	raw, _ := json.Marshal(request)
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	previous, err := s.journal.Load(ctx, request.ID)
	if err == nil {
		if previous.Owner != owner || previous.RequestDigest != digest {
			return result, domain.Fail("CONFLICT", "Writeback job ID has different parameters")
		}
		return s.recover(ctx, previous)
	}
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "NOT_FOUND" {
		return result, err
	}
	lease, err := s.coordinator.Acquire(ctx, owner, request.Target, request.SelectionRevision, request.ID)
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
	fs, err := openDisk(root, request.Target)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	source, err := fs.Open(path.Base(request.Target.RelativePath))
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	old, err := s.describe(ctx, fs, source)
	if err != nil {
		return result, err
	}
	if old.Identity != request.Target.FileIdentity || old.Version != request.Target.AssetVersion {
		return result, changed()
	}
	if old.Size > s.policy.MaxFileBytes {
		return result, unsupported("Source exceeds the writeback size limit")
	}
	now := time.Now().UTC()
	record := Record{ID: request.ID, Owner: owner, Target: request.Target, SelectionRevision: request.SelectionRevision, RequestDigest: digest, Fields: fields, RootIdentity: root.Identity, Stage: "prepared", Old: old, CreatedAt: now, UpdatedAt: now}
	if err = s.journal.Create(ctx, record); err != nil {
		return result, err
	}
	temp, err := fs.Create(stageName(record.ID), source)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, temp.Close()) }()
	originalAudio, err := s.audioDigest(ctx, source, old.Size)
	if err != nil {
		return result, err
	}
	edit, err := s.format.Rewrite(ctx, source, old.Size, temp, request.Patch)
	if err != nil {
		return result, err
	}
	if edit.AudioSHA512 != originalAudio {
		return result, changed()
	}
	if err = temp.Sync(); err != nil {
		return result, err
	}
	record.New, err = s.describe(ctx, fs, temp)
	if err != nil {
		return result, err
	}
	if err = s.verifyAudio(ctx, temp, record.New.Size, originalAudio); err != nil {
		return result, err
	}
	record.AudioSHA512, record.Changed = edit.AudioSHA512, record.New.SHA512 != record.Old.SHA512
	if err = s.save(ctx, &record, "written"); err != nil {
		return result, err
	}
	if !record.Changed {
		if err = fs.Discard(temp); err != nil {
			return result, err
		}
		record.New = record.Old
		return s.commit(ctx, lease, &record)
	}
	if err = lease.Check(ctx); err != nil {
		return result, err
	}
	state, err := fs.State(source)
	if err != nil || state.Identity != old.Identity || state.Version != old.Version {
		return result, changed()
	}
	if err = fs.Rename(source, backupName(record.ID)); err != nil {
		return result, err
	}
	if err = s.save(ctx, &record, "displaced"); err != nil {
		return result, err
	}
	return s.install(ctx, fs, lease, temp, &record)
}

func stageName(id string) string  { return ".musicfs-writeback-" + id + ".new" }
func backupName(id string) string { return ".musicfs-writeback-" + id + ".bak" }

func (s *Service) describe(ctx context.Context, fs disk, file *os.File) (FileState, error) {
	state, err := fs.State(file)
	if err != nil {
		return state, err
	}
	if state.Size < 46 || state.Size > s.policy.MaxFileBytes+maxMetadata {
		return state, unsupported("Source exceeds the writeback size limit")
	}
	h := sha512.New()
	n, err := copyContext(ctx, h, io.NewSectionReader(file, 0, state.Size))
	if err != nil {
		return state, err
	}
	if n != state.Size {
		return state, changed()
	}
	state.SHA512 = fmt.Sprintf("%x", h.Sum(nil))
	return state, nil
}

func (s *Service) save(ctx context.Context, record *Record, stage string) error {
	previous := record.Stage
	record.Stage, record.UpdatedAt = stage, time.Now().UTC()
	return s.journal.Save(ctx, previous, *record)
}

func (s *Service) install(ctx context.Context, fs disk, lease Lease, temp *os.File, record *Record) (Result, error) {
	if err := lease.Check(ctx); err != nil {
		return Result{}, err
	}
	if err := fs.Rename(temp, path.Base(record.Target.RelativePath)); err != nil {
		return Result{}, err
	}
	state, err := fs.State(temp)
	if err != nil {
		return Result{}, err
	}
	state.SHA512 = record.New.SHA512
	record.New = state
	if err = s.save(ctx, record, "installed"); err != nil {
		return Result{}, err
	}
	return s.commit(ctx, lease, record)
}

func (s *Service) commit(ctx context.Context, lease Lease, record *Record) (Result, error) {
	result := resultOf(*record)
	if err := lease.Check(ctx); err != nil {
		return result, err
	}
	if err := lease.Commit(ctx, result); err != nil {
		return result, err
	}
	return result, s.save(ctx, record, "committed")
}

func resultOf(record Record) Result {
	backup := ""
	if record.Changed {
		backup = path.Join(path.Dir(record.Target.RelativePath), backupName(record.ID))
	}
	return Result{JobID: record.ID, SourceTrackID: record.Target.SourceTrackID, Old: record.Old, New: record.New, AudioSHA512: record.AudioSHA512, BackupRelativePath: backup, Changed: record.Changed}
}

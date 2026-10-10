package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// Journal records write intent, not an alternative source of enabled settings.
type Journal interface {
	FindConfigOperation(context.Context, string, string) (Operation, bool, error)
	BeginConfigOperation(context.Context, Operation) error
	SetConfigOperationStatus(context.Context, string, string) error
	PendingConfigOperations(context.Context, string) ([]Operation, error)
}

type Operation struct {
	ID, Key, Digest, ConfigPath    string
	PreviousRevision, NextRevision string
	PluginID, InstanceID, Status   string
	Enabled                        bool
}

type Change struct {
	ExpectedRevision, IdempotencyKey string
	PluginID, InstanceID             string
	Enabled                          bool
	Mode                             string
	Settings                         *InstanceSettings
}

type Snapshot struct {
	Config          Config `json:"config"`
	Revision        string `json:"revision"`
	AppliedRevision string `json:"appliedRevision"`
	EnabledSource   string `json:"enabledSource"`
	Writable        bool   `json:"writable"`
	RestartRequired bool   `json:"restartRequired"`
	DiskChanged     bool   `json:"diskChanged"`
}

type Store struct {
	mu        sync.Mutex
	document  *Document
	applied   string
	overrides map[string]string
	journal   Journal
}

// OpenStore shares parsing with startup. It does not start or reconfigure plugins.
func OpenStore(ctx context.Context, path string, overrides map[string]string, journal Journal) (*Store, error) {
	doc, err := ReadDocument(path, overrides)
	if err != nil {
		return nil, err
	}
	s := &Store{document: doc, overrides: maps.Clone(overrides), journal: journal}
	if journal == nil {
		return s, nil
	}
	unlock, err := lockConfig(doc.Path)
	if err != nil {
		if !canWriteConfig(doc.Path) {
			return s, nil
		}
		return nil, domain.Fail("CONFLICT", "Configuration is being written; retry reading")
	}
	defer unlock()
	doc, err = ReadDocument(path, overrides)
	if err != nil {
		return nil, err
	}
	s.document = doc
	if err := s.recover(ctx, doc.Revision); err != nil {
		return nil, err
	}
	return s, nil
}

// Snapshot omits credentials even when the caller formats the value directly.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := cloneConfig(s.document.Config)
	c.AdminToken = ""
	diskChanged := s.checkDisk() != nil
	return Snapshot{Config: c, Revision: s.document.Revision, AppliedRevision: s.applied,
		EnabledSource: "yaml", Writable: s.journal != nil && canWriteConfig(s.document.Path),
		RestartRequired: s.applied != s.document.Revision || diskChanged, DiskChanged: diskChanged}
}

// Current is for trusted startup code; management callers should use Snapshot.
func (s *Store) Current() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneConfig(s.document.Config)
}

func cloneConfig(c Config) Config {
	c.SourceOverrides = maps.Clone(c.SourceOverrides)
	c.WriteBack.Roots = append([]WriteBackRoot(nil), c.WriteBack.Roots...)
	for i := range c.WriteBack.Roots {
		c.WriteBack.Roots[i].Fields = append([]string(nil), c.WriteBack.Roots[i].Fields...)
	}
	c.Metadata.Automatic.InstancePriority = append([]string(nil), c.Metadata.Automatic.InstancePriority...)
	c.Metadata.Automatic.Fields = append([]string(nil), c.Metadata.Automatic.Fields...)
	c.Metadata.Automatic.Languages = append([]string(nil), c.Metadata.Automatic.Languages...)
	c.AllowedOrigins = append([]string(nil), c.AllowedOrigins...)
	c.Plugins = append([]Plugin(nil), c.Plugins...)
	for i := range c.Plugins {
		c.Plugins[i].Args = append([]string(nil), c.Plugins[i].Args...)
		c.Plugins[i].Instances = append([]Instance(nil), c.Plugins[i].Instances...)
		for j := range c.Plugins[i].Instances {
			instance := &c.Plugins[i].Instances[j]
			if instance.Permissions != nil {
				instance.Permissions = &Permissions{ReadDirectories: append([]string{}, instance.Permissions.ReadDirectories...)}
			}
			if instance.Config.Watch != nil {
				watch := *instance.Config.Watch
				instance.Config.Watch = &watch
			}
			instance.Config.MusicRoots = append([]MusicRoot(nil), instance.Config.MusicRoots...)
			if instance.ImportDefaults != nil {
				instance.ImportDefaults = instance.ImportDefaults.Clone()
			}
		}
	}
	return c
}

// Reload validates the whole file before changing the loaded desired revision.
// Runtime application is separate: a successful read is not a successful restart.
func (s *Store) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := lockConfig(s.document.Path)
	if err != nil {
		return domain.Fail("CONFIG_READ_ONLY", "Configuration reload cannot acquire the coordination lock")
	}
	defer unlock()
	doc, err := ReadDocument(s.document.Path, s.overrides)
	if err != nil {
		return err
	}
	if err := s.recover(ctx, doc.Revision); err != nil {
		return err
	}
	s.document = doc
	return nil
}

// MarkApplied is called by a lifecycle owner only after it applies this revision.
func (s *Store) MarkApplied(ctx context.Context, revision string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if revision != s.document.Revision {
		return domain.Fail("PRECONDITION_FAILED", "Configuration revision changed")
	}
	unlock, err := lockConfig(s.document.Path)
	if err != nil {
		return domain.Fail("CONFLICT", "Cannot coordinate applied configuration revision")
	}
	defer unlock()
	if err := s.checkDisk(); err != nil {
		return err
	}
	if s.journal != nil {
		pending, err := s.journal.PendingConfigOperations(ctx, s.document.Path)
		if err != nil {
			return err
		}
		for _, op := range pending {
			status := "superseded"
			if op.NextRevision == revision && op.Status == "saved" {
				status = "applied"
			}
			if err := s.journal.SetConfigOperationStatus(ctx, op.ID, status); err != nil {
				return err
			}
		}
	}
	s.applied = revision
	return nil
}

func (s *Store) recover(ctx context.Context, revision string) error {
	if s.journal == nil {
		return nil
	}
	pending, err := s.journal.PendingConfigOperations(ctx, s.document.Path)
	if err != nil {
		return err
	}
	for _, op := range pending {
		status := "superseded"
		switch revision {
		case op.NextRevision:
			status = "saved"
		case op.PreviousRevision:
			status = "notSaved"
		}
		if err := s.journal.SetConfigOperationStatus(ctx, op.ID, status); err != nil {
			return err
		}
	}
	return nil
}

func changeDigest(change Change) string {
	data, _ := json.Marshal(change)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

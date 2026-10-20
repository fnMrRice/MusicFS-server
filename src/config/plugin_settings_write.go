package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugindb"
)

// saveLocalSettings requires the Store mutex. It never modifies the YAML file.
func (s *Store) saveLocalSettings(ctx context.Context, change Change) (Operation, error) {
	old, found, err := s.journal.FindConfigOperation(ctx, s.document.Path, change.IdempotencyKey)
	if err != nil {
		return Operation{}, err
	}
	digest := changeDigest(change)
	if found {
		if old.Digest != digest {
			return Operation{}, domain.Fail("CONFLICT", "Idempotency key has different parameters")
		}
		return old, nil
	}
	if change.ExpectedRevision != s.document.Revision {
		return Operation{}, domain.Fail("PRECONDITION_FAILED", "Configuration revision changed")
	}
	if err := s.checkDisk(); err != nil {
		return Operation{}, err
	}
	_, settings, err := prepareLocalSettings(s.document.Config, change)
	if err != nil {
		return Operation{}, err
	}
	db, err := plugindb.OpenBackend(ctx, PluginDatabaseBackend(s.document.Config), PluginDatabasePath(s.document.Config), change.PluginID, change.InstanceID)
	if err != nil {
		return Operation{}, err
	}
	defer db.Close()
	previous, err := db.Get(ctx, plugindb.ConfigurationKey)
	if err != nil {
		return Operation{}, err
	}
	current, err := ReadDocument(s.document.Path, s.overrides)
	if err != nil {
		return Operation{}, err
	}
	if current.Revision != s.document.Revision {
		return Operation{}, domain.Fail("PRECONDITION_FAILED", "Plugin configuration changed")
	}
	value, _ := json.Marshal(settings)
	next, err := s.localNextRevision(ctx, change, previous.Revision+1)
	if err != nil {
		return Operation{}, err
	}
	op := Operation{ID: domain.NewID(), Key: change.IdempotencyKey, Digest: digest, ConfigPath: s.document.Path, PreviousRevision: s.document.Revision, NextRevision: next, PluginID: change.PluginID, InstanceID: change.InstanceID, Status: "prepared"}
	if err := s.journal.BeginConfigOperation(ctx, op); err != nil {
		return Operation{}, err
	}
	if _, err := db.CompareAndSet(ctx, pluginproto.StateRequest{Key: plugindb.ConfigurationKey, Revision: previous.Revision, Value: value}); err != nil {
		return Operation{}, err
	}
	doc, err := ReadDocument(s.document.Path, s.overrides)
	if err != nil {
		return op, domain.Fail("TEMPORARY_FAILURE", "Plugin settings saved; reload is required")
	}
	s.document = doc
	op.Status = "saved"
	if err := s.journal.SetConfigOperationStatus(ctx, op.ID, "saved"); err != nil {
		return op, err
	}
	return op, nil
}

func (s *Store) localNextRevision(ctx context.Context, change Change, revision int64) (string, error) {
	result := localDirectoryRevision(s.document.Config, fmt.Sprintf("%x", sha256.Sum256(s.document.Raw)))
	for _, p := range s.document.Config.Plugins {
		if p.PluginID != "musicfs.plugin.source.localfiles" {
			continue
		}
		for _, i := range p.Instances {
			db, err := plugindb.OpenBackend(ctx, PluginDatabaseBackend(s.document.Config), PluginDatabasePath(s.document.Config), p.PluginID, i.InstanceID)
			if err != nil {
				return "", err
			}
			v, err := db.Get(ctx, plugindb.ConfigurationKey)
			db.Close()
			if err != nil {
				return "", err
			}
			if p.PluginID == change.PluginID && i.InstanceID == change.InstanceID {
				v.Revision = revision
			}
			if v.Revision > 0 {
				result = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%d", result, p.PluginID, i.InstanceID, v.Revision))))
			}
		}
	}
	return result, nil
}

package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"go.yaml.in/yaml/v3"
)

// SetEnabled persists desired state only. It never claims the runtime has changed.
func (s *Store) SetEnabled(ctx context.Context, change Change) (Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Operation{}, err
	}
	if s.journal == nil {
		return Operation{}, domain.Fail("CONFIG_READ_ONLY", "Configuration writes require a durable journal")
	}
	if len(change.IdempotencyKey) < 8 || len(change.IdempotencyKey) > 128 || len(change.ExpectedRevision) != 64 {
		return Operation{}, domain.Fail("INVALID_ARGUMENT", "Expected revision and idempotency key are required")
	}
	if err := os.MkdirAll(filepath.Dir(PluginDatabasePath(s.document.Config)), 0700); err != nil {
		return Operation{}, err
	}
	unlockDatabase, err := lockConfig(PluginDatabasePath(s.document.Config))
	if err != nil {
		return Operation{}, domain.Fail("CONFLICT", "Plugin configuration is being written")
	}
	defer unlockDatabase()
	if change.Settings != nil && pluginconfig.SupportsDirectories(change.PluginID) {
		return s.saveLocalSettings(ctx, change)
	}
	unlock, err := lockConfig(s.document.Path)
	if err != nil {
		return Operation{}, domain.Fail("CONFIG_READ_ONLY", "Configuration cannot be locked for writing")
	}
	defer unlock()
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
	current, err := ReadDocument(s.document.Path, s.overrides)
	if err != nil {
		return Operation{}, err
	}
	if current.Revision != s.document.Revision {
		return Operation{}, domain.Fail("PRECONDITION_FAILED", "Plugin configuration changed")
	}
	if !canWriteConfig(s.document.Path) {
		return Operation{}, domain.Fail("CONFIG_READ_ONLY", "Configuration file or directory is not writable")
	}
	doc, err := ParseDocument(s.document.Raw, s.document.Path, s.overrides)
	if err != nil {
		return Operation{}, err
	}
	node := enabledTarget(doc.Root, change.PluginID, change.InstanceID)
	if node == nil {
		return Operation{}, domain.Fail("NOT_FOUND", "Configured plugin or instance not found")
	}
	if change.Settings != nil {
		if err := applyInstanceSettings(doc, change, node); err != nil {
			return Operation{}, err
		}
	} else if err := setNode(node, "enabled", change.Enabled); err != nil {
		return Operation{}, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc.Root); err != nil {
		return Operation{}, domain.Fail("TEMPORARY_FAILURE", "Cannot encode configuration")
	}
	if err := encoder.Close(); err != nil {
		return Operation{}, domain.Fail("TEMPORARY_FAILURE", "Cannot finish configuration encoding")
	}
	next, err := ParseDocument(output.Bytes(), doc.Path, s.overrides)
	if err != nil {
		return Operation{}, domain.Fail("INVALID_ARGUMENT", "Plugin settings do not satisfy the configuration schema")
	}
	op := Operation{ID: domain.NewID(), Key: change.IdempotencyKey, Digest: digest, ConfigPath: doc.Path,
		PreviousRevision: s.document.Revision, NextRevision: next.Revision, PluginID: change.PluginID,
		InstanceID: change.InstanceID, Enabled: change.Enabled, Status: "prepared"}
	if err := s.journal.BeginConfigOperation(ctx, op); err != nil {
		return Operation{}, err
	}
	err = atomicWriteConfig(doc.Path, next.Raw, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return s.checkDisk()
	})
	if err != nil {
		// The journal remains prepared if the outcome is uncertain (for example fsync failure).
		return op, err
	}
	s.document = next
	op.Status = "saved"
	if err := s.journal.SetConfigOperationStatus(ctx, op.ID, op.Status); err != nil {
		return op, domain.Fail("TEMPORARY_FAILURE", "Configuration was saved; journal confirmation requires recovery")
	}
	return op, nil
}

func (s *Store) checkDisk() error {
	f, err := os.Open(s.document.Path)
	if err != nil {
		return domain.Fail("CONFIG_NOT_APPLIED", "Configuration on disk is unavailable; reload or restart first")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || sha256.Sum256(raw) != sha256.Sum256(s.document.Raw) {
		return domain.Fail("CONFIG_NOT_APPLIED", "Configuration on disk changed; reload or restart first")
	}
	return nil
}

func enabledTarget(root *yaml.Node, plugin, instance string) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	list := mappingValue(root, "plugins")
	if list == nil {
		return nil
	}
	for _, item := range list.Content {
		id := mappingValue(item, "pluginId")
		if id == nil || id.Value != plugin {
			continue
		}
		if instance == "" {
			return item
		}
		instances := mappingValue(item, "instances")
		if instances != nil {
			for _, child := range instances.Content {
				id := mappingValue(child, "instanceId")
				if id != nil && id.Value == instance {
					return child
				}
			}
		}
	}
	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

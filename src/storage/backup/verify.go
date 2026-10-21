package backup

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

const maxDatabaseBytes int64 = 64 << 30

func Verify(ctx context.Context, directory string) (Manifest, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Manifest{}, err
	}
	defer root.Close()
	bundle, err := verify(ctx, root)
	return bundle.Manifest, err
}

func checkFile(ctx context.Context, root *os.Root, want File, limit int64) error {
	hash, err := hex.DecodeString(want.SHA256)
	if err != nil || len(hash) != 32 || want.Size < 0 || want.Size > limit || !safeName(want.Path) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid backup file description")
	}
	got, err := checksumFile(ctx, root, want.Path, want.Size)
	if err != nil {
		return err
	}
	if got != want {
		return domain.Fail("RESOURCE_CHANGED", "Backup file checksum mismatch")
	}
	return nil
}

func verify(ctx context.Context, root *os.Root) (checkedBundle, error) {
	var b checkedBundle
	if err := readJSON(root, "bundle.json", &b.Manifest); err != nil {
		return b, err
	}
	m := b.Manifest
	driver, err := storage.Lookup(m.StorageBackend)
	if err != nil {
		return b, err
	}
	// Verification uses the built-in portable snapshot inspector. No executable
	// or connection options from the backed-up configuration are ever invoked.
	version, err := sqlite.BackupSchemaVersion()
	if err != nil {
		return b, err
	}
	if m.Version != 1 || m.OS != runtime.GOOS || m.Architecture != runtime.GOARCH || m.SchemaVersion != version || len(m.Files) > MaxStateFiles+3 || len(m.Files) < 3 || m.BackupID == "" {
		return b, domain.Fail("UNSUPPORTED", "Backup format, platform or database schema does not match this server")
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		if seen[strings.ToLower(f.Path)] {
			return b, domain.Fail("INVALID_ARGUMENT", "Duplicate backup file")
		}
		seen[strings.ToLower(f.Path)] = true
		limit := maxFileBytes
		switch f.Path {
		case "configuration.yaml", "configuration.source.yaml":
			limit = 1 << 20
		case "manifest.json":
			limit = maxManifestBytes
		default:
			if !strings.HasPrefix(f.Path, "state/providers/") && !strings.HasPrefix(f.Path, "state/online/") && !strings.HasPrefix(f.Path, "state/writeback/") {
				return b, domain.Fail("INVALID_ARGUMENT", "Unrecognized backup state path")
			}
			total += f.Size
			if f.Size < 0 || total > MaxStateBytes {
				return b, domain.Fail("RATE_LIMITED", "Backup state exceeds its limit")
			}
		}
		if err = checkFile(ctx, root, f, limit); err != nil {
			return b, err
		}
	}
	for _, name := range []string{"configuration.yaml", "configuration.source.yaml", "manifest.json"} {
		if !seen[name] {
			return b, domain.Fail("INVALID_ARGUMENT", "Backup configuration or media manifest is missing")
		}
	}
	if err = readJSON(root, "manifest.json", &b.Media); err != nil {
		return b, err
	}
	media := b.Media
	if media.Version != 1 || media.BackupID != m.BackupID || media.Database.Name != driver.SnapshotName || len(media.Objects) > mediastore.MaxBackupObjects {
		return b, domain.Fail("INVALID_ARGUMENT", "Invalid media backup manifest")
	}
	if err = checkFile(ctx, root, File{Path: media.Database.Name, Size: media.Database.Size, SHA256: media.Database.SHA256}, maxDatabaseBytes); err != nil {
		return b, err
	}
	refs, err := sqlite.InspectBackup(ctx, filepath.Join(root.Name(), media.Database.Name))
	if err != nil {
		return b, err
	}
	if len(refs) != len(media.Objects) {
		return b, domain.Fail("RESOURCE_CHANGED", "Backup media reference set differs from the database")
	}
	expected := map[string]mediastore.BackupObject{}
	for _, item := range refs {
		expected[item.Object.ID] = item
	}
	missing := 0
	for i := range b.Media.Objects {
		item := &b.Media.Objects[i]
		item.Object.RelativePath = "objects/" + item.Object.ID
		want, ok := expected[item.Object.ID]
		if !ok || want.Owner != item.Owner || want.PublicationID != item.PublicationID || want.Object != item.Object || item.Object.Validate() != nil {
			return b, domain.Fail("RESOURCE_CHANGED", "Backup media reference is invalid")
		}
		delete(expected, item.Object.ID)
		switch item.State {
		case "missing":
			missing++
		case "copied":
			if err = checkFile(ctx, root, File{Path: "media/objects/" + item.Object.ID, Size: item.Object.Size, SHA256: item.Object.SHA256}, mediastore.MaxObjectBytes); err != nil {
				return b, err
			}
		default:
			return b, domain.Fail("INVALID_ARGUMENT", "Backup contains unfinished media")
		}
	}
	if missing != media.MissingCount || missing != m.MissingMedia {
		return b, domain.Fail("INVALID_ARGUMENT", "Backup missing-media count differs")
	}
	return b, nil
}

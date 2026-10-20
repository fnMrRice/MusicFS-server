package mediastore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const MaxBackupObjects = 100000

type BackupManifest struct {
	Version      int            `json:"version"`
	BackupID     string         `json:"backupId"`
	CreatedAt    time.Time      `json:"createdAt"`
	Database     BackupFile     `json:"database"`
	Objects      []BackupObject `json:"objects"`
	MissingCount int            `json:"missingCount"`
}

type BackupFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"sizeBytes"`
	SHA256 string `json:"sha256"`
}

type BackupResult struct {
	Backup    Backup         `json:"backup"`
	Directory string         `json:"-"`
	Manifest  BackupManifest `json:"manifest"`
}

type Backups struct {
	repo         BackupRepository
	files        *Files
	databaseName string
}

func NewBackups(repo BackupRepository, files *Files) *Backups {
	return &Backups{repo: repo, files: files, databaseName: "db.sqlite"}
}

// NewNamedBackups selects the registered backend's local snapshot artifact.
func NewNamedBackups(repo BackupRepository, files *Files, databaseName string) *Backups {
	return &Backups{repo: repo, files: files, databaseName: databaseName}
}

// Create uses a new private directory. The final manifest is the completeness
// marker; an interrupted directory without it must never be used as a restore.
func (b *Backups) Create(ctx context.Context, parent string) (BackupResult, error) {
	var result BackupResult
	if !filepath.IsAbs(parent) {
		return result, domain.Fail("INVALID_ARGUMENT", "Backup parent must be absolute")
	}
	if b.databaseName == "" || !filepath.IsLocal(b.databaseName) || filepath.Base(b.databaseName) != b.databaseName || b.databaseName == "." {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid backup database name")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// The caller creates the parent so success never depends on syncing unknown
	// ancestors outside the backup location.
	info, err := os.Lstat(parent)
	if err != nil {
		return result, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return result, domain.Fail("INVALID_ARGUMENT", "Backup parent must be an existing regular directory")
	}
	base, err := os.OpenRoot(parent)
	if err != nil {
		return result, err
	}
	defer base.Close()
	id := domain.NewID()
	if err = base.Mkdir(id, 0700); err != nil {
		return result, err
	}
	result.Directory = filepath.Join(parent, id)
	root, err := base.OpenRoot(id)
	if err != nil {
		return result, err
	}
	defer root.Close()
	backup, err := b.repo.CreateMediaBackup(ctx, id, filepath.Join(result.Directory, b.databaseName))
	if err != nil {
		return result, err
	}
	if backup.ObjectCount > MaxBackupObjects {
		return result, domain.Fail("RATE_LIMITED", "Backup object count exceeds this implementation's bound")
	}
	dest, err := OpenFiles(filepath.Join(result.Directory, "media"))
	if err != nil {
		return result, err
	}
	defer dest.Close()
	manifest := BackupManifest{Version: 1, BackupID: id, CreatedAt: backup.CreatedAt, Objects: []BackupObject{}}
	manifest.Database, err = backupDatabase(ctx, root, b.databaseName)
	if err != nil {
		return result, err
	}
	after := ""
	for {
		objects, err := b.repo.BackupObjects(ctx, id, after, 128)
		if err != nil {
			return result, err
		}
		if len(objects) == 0 {
			break
		}
		for _, object := range objects {
			if err = ctx.Err(); err != nil {
				return result, err
			}
			if len(manifest.Objects) >= MaxBackupObjects {
				return result, domain.Fail("RATE_LIMITED", "Backup manifest exceeds its limit")
			}
			object.State, err = b.copyObject(ctx, dest, object.Object)
			if err != nil {
				return result, err
			}
			if err = b.repo.RecordBackupObject(ctx, id, object.Object.ID, object.State); err != nil {
				return result, err
			}
			if object.State == "missing" {
				manifest.MissingCount++
			}
			manifest.Objects = append(manifest.Objects, object)
			after = object.Object.ID
		}
	}
	if len(manifest.Objects) != backup.ObjectCount {
		return result, domain.Fail("RESOURCE_CHANGED", "Backup references changed")
	}
	if err = syncDirectory(dest.root, "."); err != nil {
		return result, err
	}
	if err = syncDirectory(root, "."); err != nil {
		return result, err
	}
	if err = syncDirectory(base, "."); err != nil {
		return result, err
	}
	if err = writeBackupManifest(ctx, root, manifest); err != nil {
		return result, err
	}
	if err = b.repo.FinishMediaBackup(ctx, id); err != nil {
		return result, err
	}
	backup.State, backup.MissingCount, backup.UpdatedAt = "finished", manifest.MissingCount, time.Now().UTC()
	result.Backup, result.Manifest = backup, manifest
	return result, nil
}

func (b *Backups) copyObject(ctx context.Context, dest *Files, object Object) (string, error) {
	in, err := b.files.Open(object)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "missing", nil
	}
	defer in.Close()
	verify := func(file io.ReadSeeker) error { return verifyBackupBytes(ctx, file, object.Size, object.SHA256) }
	copy, err := dest.Write(ctx, object.ID, object.Kind, object.MIME, in, object.Size, verify)
	if err != nil {
		return "", err
	}
	if copy != object {
		return "", domain.Fail("RESOURCE_CHANGED", "Copied backup object differs from its reference")
	}
	if err = dest.Publish(copy); err != nil {
		return "", err
	}
	return "copied", nil
}

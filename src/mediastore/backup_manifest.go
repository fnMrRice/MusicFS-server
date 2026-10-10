package mediastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func verifyBackupBytes(ctx context.Context, in io.Reader, size int64, expected string) error {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx: ctx, r: in}, size+1))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != expected {
		return domain.Fail("RESOURCE_CHANGED", "Backup content checksum differs")
	}
	return nil
}

func backupDatabase(ctx context.Context, root *os.Root) (BackupFile, error) {
	result := BackupFile{Name: "db.sqlite"}
	info, err := root.Lstat(result.Name)
	if err != nil || !info.Mode().IsRegular() {
		return result, domain.Fail("RESOURCE_CHANGED", "Backup database is not a regular file")
	}
	file, err := root.OpenFile(result.Name, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	defer file.Close()
	h := sha256.New()
	result.Size, err = io.Copy(h, contextReader{ctx: ctx, r: file})
	if err != nil {
		return result, err
	}
	result.SHA256 = hex.EncodeToString(h.Sum(nil))
	if err = file.Sync(); err != nil {
		return result, err
	}
	return result, nil
}

func writeBackupManifest(ctx context.Context, root *os.Root, manifest BackupManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := root.OpenFile("manifest.pending", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(manifest); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return publishManifest(root)
}

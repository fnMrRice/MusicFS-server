package backup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"go.yaml.in/yaml/v3"
)

// Restore writes only to a newly created directory. Failures leave a blocked
// partial restore for inspection; no old installation is overwritten or deleted.
func Restore(ctx context.Context, directory, destination string) (RestoreResult, error) {
	var result RestoreResult
	if !filepath.IsAbs(directory) || !filepath.IsAbs(destination) {
		return result, domain.Fail("INVALID_ARGUMENT", "Restore paths must be absolute")
	}
	src, err := os.OpenRoot(directory)
	if err != nil {
		return result, err
	}
	defer src.Close()
	b, err := verify(ctx, src)
	if err != nil {
		return result, err
	}
	parent, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return result, err
	}
	defer parent.Close()
	if !disjoint(directory, destination) {
		return result, domain.Fail("INVALID_ARGUMENT", "Restore must use an independent destination")
	}
	if err = parent.Mkdir(filepath.Base(destination), 0700); err != nil {
		return result, domain.Fail("CONFLICT", "Restore destination must not exist")
	}
	dst, err := parent.OpenRoot(filepath.Base(destination))
	if err != nil {
		return result, err
	}
	defer dst.Close()
	if _, err = copyFile(ctx, strings.NewReader(b.Manifest.BackupID), dst, "data/.restore-incomplete", 128); err != nil {
		return result, err
	}
	// Persist the restore barrier and its newly created ancestors before copying
	// anything that a subsequent Server could mistake for a live database.
	for _, name := range []string{"data", "."} {
		if err = syncDirectory(dst, name); err != nil {
			return result, err
		}
	}
	if err = syncDirectory(parent, "."); err != nil {
		return result, err
	}
	copyChecked := func(from, to string, want File) error {
		in, err := regular(src, from)
		if err != nil {
			return err
		}
		defer in.Close()
		actual, err := copyFile(ctx, in, dst, to, want.Size)
		if err != nil {
			return err
		}
		if actual.Size != want.Size || actual.SHA256 != want.SHA256 {
			return domain.Fail("RESOURCE_CHANGED", "Backup changed while restoring")
		}
		return nil
	}
	if err = copyChecked("db.sqlite", "data/musicfs.sqlite", File{Size: b.Media.Database.Size, SHA256: b.Media.Database.SHA256}); err != nil {
		return result, err
	}
	for _, file := range b.Manifest.Files {
		to := ""
		switch file.Path {
		case "configuration.yaml":
			to = "configuration.original.yaml"
		case "configuration.source.yaml":
			to = "configuration.source.yaml"
		case "manifest.json":
			continue
		default:
			to = "data/" + strings.TrimPrefix(file.Path, "state/")
		}
		if err = copyChecked(file.Path, to, file); err != nil {
			return result, err
		}
	}
	for _, object := range b.Media.Objects {
		if object.State == "missing" {
			continue
		}
		name := "media/objects/" + object.Object.ID
		if err = copyChecked(name, name, File{Size: object.Object.Size, SHA256: object.Object.SHA256}); err != nil {
			return result, err
		}
	}
	for _, dir := range []string{"cache", "media/staging", "media/objects"} {
		if err = dst.MkdirAll(dir, 0700); err != nil {
			return result, err
		}
	}
	store, err := sqlite.OpenMaintenance(ctx, filepath.Join(destination, "data", "musicfs.sqlite"))
	if err != nil {
		return result, err
	}
	err = store.PrepareRestoredDatabase(ctx, b.Media.Objects)
	closeErr := store.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	if err = restoredConfig(ctx, dst, destination); err != nil {
		return result, err
	}
	result = RestoreResult{Directory: destination, Configuration: filepath.Join(destination, "configuration.yaml"), BackupID: b.Manifest.BackupID, MissingMedia: b.Manifest.MissingMedia, RequiresLogin: true}
	if _, err = jsonFile(ctx, dst, "restore.json", result); err != nil {
		return result, err
	}
	if err = syncTree(dst); err != nil {
		return result, err
	}
	if err = dst.Remove("data/.restore-incomplete"); err != nil {
		return result, err
	}
	if err = syncDirectory(dst, "data"); err != nil {
		return result, err
	}
	if err = syncDirectory(parent, "."); err != nil {
		return result, err
	}
	return result, nil
}

func restoredConfig(ctx context.Context, dst *os.Root, destination string) error {
	in, err := regular(dst, "configuration.original.yaml")
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(in, config.MaxDocumentBytes+1))
	in.Close()
	if err != nil {
		return err
	}
	var c config.Config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err = decoder.Decode(&c); err != nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid backed-up configuration")
	}
	c.NormalizeIdentities()
	c.DataRoot = filepath.Join(destination, "data")
	c.MediaRoot = filepath.Join(destination, "media")
	c.CacheRoot = filepath.Join(destination, "cache")
	c.Listen = "127.0.0.1:8080"
	c.AllowLoopbackHTTP = true
	c.AllowedOrigins = []string{"http://127.0.0.1:8080"}
	c.TLSCert = ""
	c.TLSKey = ""
	c.WebRoot = ""
	c.WriteBack.Enabled = false
	c.WriteBack.AllowOverwrite = false
	c.Hashing.Auto = false
	c.Fingerprinting.Enabled = false
	c.Metadata.Automatic.Enabled = false
	for i := range c.Plugins {
		c.Plugins[i].Enabled = false
		for j := range c.Plugins[i].Instances {
			c.Plugins[i].Instances[j].Enabled = false
		}
	}
	c.AdminToken = domain.NewID() // Validation only: AdminToken is excluded from YAML.
	if err = c.Validate(); err != nil {
		return err
	}
	raw, err = yaml.Marshal(c)
	if err != nil {
		return err
	}
	_, err = copyFile(ctx, bytes.NewReader(raw), dst, "configuration.yaml", config.MaxDocumentBytes)
	return err
}

package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"go.yaml.in/yaml/v3"
)

// Create requires a stopped installation and an existing independent parent.
// A bundle is complete only when bundle.json and all its checksums verify.
func Create(ctx context.Context, doc *config.Document, parent string) (Result, error) {
	var result Result
	if doc == nil {
		return result, domain.Fail("INVALID_ARGUMENT", "Configuration is required")
	}
	c := doc.Config
	if err := c.Validate(); err != nil {
		return result, err
	}
	if !filepath.IsAbs(parent) {
		return result, domain.Fail("INVALID_ARGUMENT", "Backup parent must be absolute")
	}
	for _, dir := range []string{c.DataRoot, c.MediaRoot, c.CacheRoot} {
		if !disjoint(parent, dir) {
			return result, domain.Fail("INVALID_ARGUMENT", "Backup destination overlaps live storage")
		}
	}
	for _, plugin := range c.Plugins {
		for _, instance := range plugin.Instances {
			for _, root := range instance.Config.MusicRoots {
				if !disjoint(parent, root.Path) {
					return result, domain.Fail("INVALID_ARGUMENT", "Backup destination overlaps music")
				}
			}
		}
	}
	state, err := os.OpenRoot(c.DataRoot)
	if err != nil {
		return result, err
	}
	defer state.Close()
	if _, err = state.Lstat(".restore-incomplete"); !os.IsNotExist(err) {
		return result, domain.Fail("CONFLICT", "Installation restore is incomplete")
	}
	lock, err := state.OpenFile("server.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return result, err
	}
	defer lock.Close()
	if err = lockFile(lock); err != nil {
		return result, domain.Fail("CONFLICT", "Stop the server before creating a backup")
	}
	store, err := sqlite.OpenMaintenance(ctx, filepath.Join(c.DataRoot, "musicfs.sqlite"))
	if err != nil {
		return result, err
	}
	defer store.Close()
	var pages, pageSize int64
	if err = store.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return result, err
	}
	if err = store.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return result, err
	}
	if pageSize < 1 || pages > maxDatabaseBytes/pageSize {
		return result, domain.Fail("RATE_LIMITED", "Backup database exceeds its size limit")
	}
	files, err := mediastore.OpenFiles(c.MediaRoot)
	if err != nil {
		return result, err
	}
	defer files.Close()
	media, err := mediastore.NewBackups(store, files).Create(ctx, parent)
	if err != nil {
		return result, err
	}
	if media.Manifest.Database.Size > maxDatabaseBytes {
		return result, domain.Fail("RATE_LIMITED", "Backup database exceeds its size limit")
	}
	root, err := os.OpenRoot(media.Directory)
	if err != nil {
		return result, err
	}
	defer root.Close()
	version, err := sqlite.BackupSchemaVersion()
	if err != nil {
		return result, err
	}
	m := Manifest{Version: 1, BackupID: media.Backup.ID, CreatedAt: time.Now().UTC(), OS: runtime.GOOS, Architecture: runtime.GOARCH, SchemaVersion: version, Files: []File{}, MissingMedia: media.Manifest.MissingCount, ExternalCredentials: []string{"MUSICFS_ADMIN_TOKEN", "TLS private key and certificate (when configured)"}}
	appendFile := func(file File, err error) error {
		if err == nil {
			m.Files = append(m.Files, file)
		}
		return err
	}
	if err = appendFile(copyFile(ctx, bytes.NewReader(doc.Raw), root, "configuration.source.yaml", config.MaxDocumentBytes)); err != nil {
		return result, err
	}
	effective, err := yaml.Marshal(c)
	if err != nil {
		return result, err
	}
	if err = appendFile(copyFile(ctx, bytes.NewReader(effective), root, "configuration.yaml", config.MaxDocumentBytes)); err != nil {
		return result, err
	}
	if err = appendFile(checksumFile(ctx, root, "manifest.json", maxManifestBytes)); err != nil {
		return result, err
	}
	var stateBytes int64
	for _, directory := range []string{"providers", "online", "writeback"} {
		if err = copyState(ctx, state, root, directory, &m.Files, &stateBytes); err != nil {
			return result, err
		}
	}
	// A concurrent manual config edit must not produce an apparently consistent
	// bundle. The runtime configuration and the original document are both kept.
	current, err := os.ReadFile(doc.Path)
	if err != nil || !bytes.Equal(current, doc.Raw) {
		return result, domain.Fail("RESOURCE_CHANGED", "Configuration changed during backup")
	}
	m.Dependencies = dependencies(ctx, c)
	if err = syncTree(root); err != nil {
		return result, err
	}
	if _, err = jsonFile(ctx, root, "bundle.json", m); err != nil {
		return result, err
	}
	if err = syncDirectory(root, "."); err != nil {
		return result, err
	}
	return Result{Directory: media.Directory, Manifest: m}, nil
}

func dependencies(ctx context.Context, c config.Config) []Dependency {
	items := []Dependency{{Name: "musicfs", Version: "0.1.0/API-v1"}, {Name: "go", Version: runtime.Version()}}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, module := range info.Deps {
			version := module.Version
			if module.Replace != nil {
				version = module.Replace.Version
			}
			items = append(items, Dependency{Name: module.Path, Version: version})
		}
	}
	add := func(name, path string) {
		entry := Dependency{Name: name, Version: "unreported", State: "unavailable"}
		if path != "" {
			if root, err := os.OpenRoot(filepath.Dir(path)); err == nil {
				if file, err := checksumFile(ctx, root, filepath.Base(path), 512<<20); err == nil {
					entry.SHA256 = file.SHA256
					entry.State = "recorded"
				}
				root.Close()
			}
		}
		items = append(items, entry)
	}
	for _, plugin := range c.Plugins {
		add(plugin.PluginID, plugin.Executable)
		for _, instance := range plugin.Instances {
			if plugin.PluginID == "musicfs.plugin.decoder.ffmpeg" {
				add(instance.InstanceID+"/ffmpeg", instance.Config.FFmpegPath)
				add(instance.InstanceID+"/ffprobe", instance.Config.FFprobePath)
			}
		}
	}
	if c.Fingerprinting.FpcalcPath != "" {
		add("fpcalc", c.Fingerprinting.FpcalcPath)
	}
	return items
}

package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	_ "github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
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
	driver, err := storage.Lookup(c.Storage.Backend)
	if err != nil {
		return result, err
	}
	store, err := storage.OpenMaintenance(ctx, driver.Name, storage.Options{
		DataRoot: c.DataRoot, Values: c.Storage.Options,
		Executable: c.Storage.Executable, Args: c.Storage.Args, SHA256: c.Storage.SHA256,
	})
	if err != nil {
		return result, err
	}
	defer store.Close()
	databaseSize, err := store.DatabaseSize(ctx)
	if err != nil {
		return result, err
	}
	if databaseSize < 0 || databaseSize > maxDatabaseBytes {
		return result, domain.Fail("RATE_LIMITED", "Backup database exceeds its size limit")
	}
	files, err := mediastore.OpenFiles(c.MediaRoot)
	if err != nil {
		return result, err
	}
	defer files.Close()
	media, err := mediastore.NewNamedBackups(store, files, driver.SnapshotName).Create(ctx, parent)
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
	version, err := driver.BackupSchemaVersion()
	if err != nil {
		return result, err
	}
	m := Manifest{Version: 1, BackupID: media.Backup.ID, CreatedAt: time.Now().UTC(), OS: runtime.GOOS, Architecture: runtime.GOARCH, SchemaVersion: version, StorageBackend: driver.Name, Files: []File{}, MissingMedia: media.Manifest.MissingCount, ExternalCredentials: []string{"MUSICFS_ADMIN_TOKEN", "TLS private key and certificate (when configured)"}}
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
			if pluginconfig.NeedsAudioTools(plugin.PluginID) {
				add(instance.InstanceID+"/ffmpeg", instance.Config.FFmpegPath)
				add(instance.InstanceID+"/ffprobe", instance.Config.FFprobePath)
			}
		}
	}
	if c.Fingerprinting.FpcalcPath != "" {
		add("fpcalc", c.Fingerprinting.FpcalcPath)
	}
	if c.Storage.Backend == "plugin" {
		add("storage", c.Storage.Executable)
	}
	for _, dsp := range c.AudioDSP.Plugins {
		add("dsp/"+dsp.Name, dsp.Executable)
	}
	return items
}

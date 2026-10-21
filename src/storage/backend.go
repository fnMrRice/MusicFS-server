// Package storage defines the trusted persistence boundary of the server.
// Implementations preserve each repository operation's authorization, revision,
// idempotency and atomic publication semantics; they are not arbitrary SQL APIs.
package storage

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/library/sourcedisplay"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

const ContractVersion = 1

// Backend is one consistency domain. A factory completes schema and identity
// migrations before returning it, and must never return a partially ready store.
// Callback-bearing operations retain their guard for the complete transaction.
// Implementations must not acknowledge durable writes before commit succeeds.
type Backend interface {
	auth.Repository
	albums.Repository
	catalog.Repository
	config.Journal
	enrichment.Repository
	enrichment.AutomationRepository
	identification.Repository
	identification.HashRepository
	identification.FingerprintRepository
	jobs.Repository
	library.Repository
	matching.Repository
	mediastore.Repository
	mediastore.ContentRepository
	mediastore.DerivedRepository
	mediastore.GarbageRepository
	personal.Repository
	playback.Repository
	playlists.Repository
	playlists.AggregateRepository
	plugincontrol.Repository
	processedcache.Repository
	sourcedisplay.Repository
	timeline.Repository
	writebackhost.Repository
	Close() error
}

// Availability is an optional host-local health signal, outside the repository
// wire contract. Implementations must return immediately without I/O or retries.
type Availability interface {
	Available() bool
}

// Maintenance never creates or migrates live data. Callers hold server.lock, or
// exclusively own a fresh restore directory. A snapshot includes the exact media
// reference set exported by BackupRepository, not a later live query.
type Maintenance interface {
	mediastore.BackupRepository
	DatabaseSize(context.Context) (int64, error)
	PrepareRestoredDatabase(context.Context, []mediastore.BackupObject) error
	Close() error
}

type Options struct {
	DataRoot   string
	Values     map[string]string
	Executable string
	Args       []string
	SHA256     string
}

// Driver supplies both runtime and offline maintenance support. Contract v1
// snapshots use the server's portable SQLite schema regardless of runtime
// database technology. Restore therefore never executes a backed-up plugin.
// Backup names describe local artifacts, not database connection strings.
type Driver struct {
	Name                string
	Version             int
	PluginVersion       string
	SnapshotName        string
	RestoredName        string
	Open                func(context.Context, Options) (Backend, error)
	OpenMaintenance     func(context.Context, Options) (Maintenance, error)
	BackupSchemaVersion func() (int, error)
	InspectBackup       func(context.Context, string) ([]mediastore.BackupObject, error)
}

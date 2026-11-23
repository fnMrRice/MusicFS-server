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
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
	"github.com/fnMrRice/MusicFS-server/src/library/sourcedisplay"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

const ContractVersion = 1

// Backend is one consistency domain. A factory completes schema and identity
// migrations before returning it, and must never return a partially ready store.
// Callback-bearing operations retain their guard for the complete transaction.
// Implementations must not acknowledge durable writes before commit succeeds.
type Backend interface {
	// BindMusicSourcePlugins 保存来源实例与插件的身份映射，供插件内编号解析使用。
	BindMusicSourcePlugins(context.Context, map[string]string) error
	auth.Repository
	albums.Repository
	catalog.Repository
	music.Repository
	config.Journal
	enrichment.Repository
	enrichment.AutomationRepository
	identification.Repository
	identification.HashRepository
	identification.FingerprintRepository
	jobs.Repository
	tasks.Repository
	library.Repository
	library.AudioRepository
	library.GroupedRepository
	library.SelectionRepository
	library.RecordingCoverRepository
	matching.Repository
	matching.AutomationRepository
	settings.Repository
	mediastore.Repository
	mediastore.ContentRepository
	mediastore.DerivedRepository
	mediastore.GarbageRepository
	personal.Repository
	playback.Repository
	playlists.Repository
	playlists.LinkRepository
	playlists.AggregateRepository
	plugincontrol.Repository
	processedcache.Repository
	onlinecache.Repository
	recommendations.Repository
	sourcedisplay.Repository
	timeline.Repository
	writebackhost.Repository
	works.Repository
	workpackages.Repository
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

// Driver 提供运行、离线维护与插件私有状态存储，负责主库和私有状态的初始化及迁移。
// v1 备份契约统一使用可移植的 SQLite 结构，与运行时数据库类型无关；
// 恢复不执行备份中的插件，备份名称表示本地文件而非数据库连接字符串。
type Driver struct {
	Name                string
	Version             int
	PluginVersion       string
	SnapshotName        string
	RestoredName        string
	Open                func(context.Context, Options) (Backend, error)
	OpenMaintenance     func(context.Context, Options) (Maintenance, error)
	OpenPluginState     plugindb.OpenFactory
	ReadPluginState     plugindb.ReadFactory
	BackupSchemaVersion func() (int, error)
	InspectBackup       func(context.Context, string) ([]mediastore.BackupObject, error)
}

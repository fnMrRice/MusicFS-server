// Package backup implements offline, bounded installation backup and restore.
package backup

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

const MaxStateFiles = 100000
const MaxStateBytes int64 = 8 << 30
const maxFileBytes int64 = 1 << 30
const maxManifestBytes int64 = 64 << 20

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"sizeBytes"`
	SHA256 string `json:"sha256"`
}
type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
	State   string `json:"state,omitempty"`
}
type Manifest struct {
	Version             int          `json:"version"`
	BackupID            string       `json:"backupId"`
	CreatedAt           time.Time    `json:"createdAt"`
	OS                  string       `json:"os"`
	Architecture        string       `json:"architecture"`
	SchemaVersion       int          `json:"schemaVersion"`
	StorageBackend      string       `json:"storageBackend,omitempty"`
	Files               []File       `json:"files"`
	Dependencies        []Dependency `json:"dependencies"`
	ExternalCredentials []string     `json:"externalCredentials"`
	MissingMedia        int          `json:"missingMedia"`
}
type Result struct {
	Directory string   `json:"directory"`
	Manifest  Manifest `json:"manifest"`
}
type RestoreResult struct {
	Directory        string `json:"directory"`
	Configuration    string `json:"configuration"`
	BackupID         string `json:"backupId"`
	MissingMedia     int    `json:"missingMedia"`
	RequiresLogin    bool   `json:"requiresLogin"`
	PluginsEnabled   bool   `json:"pluginsEnabled"`
	WriteBackEnabled bool   `json:"writeBackEnabled"`
}
type checkedBundle struct {
	Manifest Manifest
	Media    mediastore.BackupManifest
}

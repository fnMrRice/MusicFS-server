package mediastore

import (
	"context"
	"time"
)

// Garbage identifies only paths assigned by an aborted database publication.
// Directory enumeration and user-provided names cannot produce a deletion task.
type Garbage struct {
	ID            string    `json:"garbageId"`
	PublicationID string    `json:"publicationId"`
	ObjectID      string    `json:"objectId"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type GarbageRepository interface {
	ClaimMediaGarbage(context.Context, time.Time, int) ([]Garbage, error)
	DeleteMediaGarbage(context.Context, string, func(Garbage) error) error
}

type Backup struct {
	ID           string    `json:"backupId"`
	State        string    `json:"state"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	ObjectCount  int       `json:"objectCount"`
	MissingCount int       `json:"missingCount"`
}

type BackupObject struct {
	Owner         string `json:"owner"`
	PublicationID string `json:"publicationId"`
	Object        Object `json:"object"`
	State         string `json:"state"`
}

// Backup references are captured from the immutable database snapshot itself.
// Every object remains pinned, including missing files and historical previews.
type BackupRepository interface {
	CreateMediaBackup(context.Context, string, string) (Backup, error)
	BackupObjects(context.Context, string, string, int) ([]BackupObject, error)
	RecordBackupObject(context.Context, string, string, string) error
	FinishMediaBackup(context.Context, string) error
}

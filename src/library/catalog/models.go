// Package catalog exposes owner-scoped, immutable local-library query results.
package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

const MaxPageSize = 200
const MaxItems = 100000
const MaxSnapshotBytes = 64 << 20
const SnapshotTTL = 10 * time.Minute

type Query struct {
	Type               string   `json:"type"`
	CategoryKind       string   `json:"categoryKind,omitempty"`
	CategoryID         string   `json:"categoryId,omitempty"`
	ParentID           string   `json:"parentId,omitempty"`
	ProviderInstanceID string   `json:"providerInstanceId"`
	Q                  string   `json:"q"`
	ArtistID           string   `json:"artistId"`
	SourceAlbumID      string   `json:"sourceAlbumId"`
	RootID             string   `json:"rootId"`
	DirectoryID        string   `json:"directoryId"`
	Format             string   `json:"format"`
	Genres             []string `json:"genres"`
	Formats            []string `json:"formats"`
	Years              []int64  `json:"years"`
	SampleRatesHz      []int64  `json:"sampleRatesHz"`
	Availability       []string `json:"availability"`
	UnknownFields      []string `json:"unknownFields"`
	FavoriteOnly       bool     `json:"favoriteOnly"`
	AvailableOnly      bool     `json:"availableOnly"`
	MinBitrateBps      *int64   `json:"minBitrateBps"`
	MaxBitrateBps      *int64   `json:"maxBitrateBps"`
	Sort               string   `json:"sort"`
	Order              string   `json:"order"`
}

type CreateRequest struct {
	Query Query `json:"query"`
	Limit int   `json:"limit"`
}

type Page struct {
	QueryID         string            `json:"queryId"`
	Query           Query             `json:"query"`
	CatalogRevision string            `json:"catalogRevision"`
	ExpiresAt       time.Time         `json:"expiresAt"`
	Total           int               `json:"total"`
	Items           []json.RawMessage `json:"items"`
	NextCursor      *string           `json:"nextCursor"`
}

type Track struct {
	library.Track
	Source      playlists.SourceRef `json:"source"`
	RootID      string              `json:"rootId"`
	DirectoryID string              `json:"directoryId"`
	Favorite    bool                `json:"favorite"`
}

// Artist identifies an exact local credit within one provider, not a verified
// real-world person. Identical credits in different providers never merge.
type Artist struct {
	ID                 string  `json:"sourceArtistId"`
	ProviderInstanceID string  `json:"providerInstanceId"`
	Name               string  `json:"name"`
	IdentityKind       string  `json:"identityKind"`
	TrackCount         int     `json:"trackCount"`
	AlbumCount         int     `json:"albumCount"`
	AvatarObjectID     *string `json:"avatarObjectId"`
}

type Album struct {
	ID                 string   `json:"sourceAlbumId"`
	ProviderInstanceID string   `json:"providerInstanceId"`
	Name               string   `json:"name"`
	Revision           int64    `json:"revision"`
	Artists            []string `json:"artists"`
	TrackCount         int      `json:"trackCount"`
	CoverObjectID      *string  `json:"coverObjectId"`
}

type Directory struct {
	ID                 string  `json:"directoryId"`
	ParentID           *string `json:"parentId"`
	ProviderInstanceID string  `json:"providerInstanceId"`
	RootID             string  `json:"rootId"`
	Name               string  `json:"name"`
	TrackCount         int     `json:"trackCount"`
}

type Format struct {
	Name       string `json:"name"`
	TrackCount int    `json:"trackCount"`
}

type TextFacet struct {
	Value      string `json:"value"`
	TrackCount int    `json:"trackCount"`
}
type NumberFacet struct {
	Value      int64 `json:"value"`
	TrackCount int   `json:"trackCount"`
}

type Facets struct {
	CatalogRevision   string         `json:"catalogRevision"`
	Directories       []Directory    `json:"directories"`
	Formats           []Format       `json:"formats"`
	Genres            []TextFacet    `json:"genres"`
	Years             []NumberFacet  `json:"years"`
	SampleRatesHz     []NumberFacet  `json:"sampleRatesHz"`
	UnknownCounts     map[string]int `json:"unknownCounts"`
	UnsupportedFields []string       `json:"unsupportedFields"`
}

type Repository interface {
	CreateCatalogQuery(context.Context, string, Query, int, string) (Page, error)
	CatalogQueryPage(context.Context, string, string, string, int, *Query) (Page, error)
	CancelCatalogQuery(context.Context, string, string) error
	CatalogFacets(context.Context, string, string) (Facets, error)
	CatalogArtist(context.Context, string, string) (Artist, error)
}

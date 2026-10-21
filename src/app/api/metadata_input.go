package api

import (
	"bytes"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

// An omitted optional list uses the service default; explicit null is not a list.
type metadataList []string

func (v *metadataList) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return domain.Fail("INVALID_ARGUMENT", "Metadata lists cannot be null")
	}
	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*v = decoded
	return nil
}

type metadataJobInput struct {
	SourceTrackIDs     metadataList    `json:"sourceTrackIds"`
	SourceAlbum        json.RawMessage `json:"sourceAlbum"`
	Fields             metadataList    `json:"fields"`
	Languages          metadataList    `json:"languages"`
	InstanceIDs        metadataList    `json:"instanceIds"`
	ExcludeInstanceIDs metadataList    `json:"excludeInstanceIds"`
	Query              *struct {
		Title   string       `json:"title"`
		Artists metadataList `json:"artists"`
	} `json:"query"`
}

func (input metadataJobInput) album() (*enrichment.SourceAlbumRequest, error) {
	if len(input.SourceAlbum) == 0 {
		return nil, nil
	}
	if input.SourceTrackIDs != nil || bytes.Equal(bytes.TrimSpace(input.SourceAlbum), []byte("null")) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Choose sourceTrackIds or an explicit sourceAlbum")
	}
	var raw struct {
		SourceAlbumID    *string `json:"sourceAlbumId"`
		ExpectedRevision *int64  `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input.SourceAlbum))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || raw.SourceAlbumID == nil || raw.ExpectedRevision == nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Source album identity and expected revision are required")
	}
	result := &enrichment.SourceAlbumRequest{SourceAlbumID: *raw.SourceAlbumID, ExpectedRevision: *raw.ExpectedRevision}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

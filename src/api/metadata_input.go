package api

import (
	"bytes"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/domain"
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
	SourceTrackIDs     metadataList `json:"sourceTrackIds"`
	Fields             metadataList `json:"fields"`
	Languages          metadataList `json:"languages"`
	InstanceIDs        metadataList `json:"instanceIds"`
	ExcludeInstanceIDs metadataList `json:"excludeInstanceIds"`
	Query              *struct {
		Title   string       `json:"title"`
		Artists metadataList `json:"artists"`
	} `json:"query"`
}

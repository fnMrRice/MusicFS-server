package ncmdecoder

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const channelChunk = 64 * 1024

type openRequest struct {
	TaskID   string `json:"taskId"`
	AssetID  string `json:"assetId"`
	Nonce    string `json:"nonce"`
	InputURL string `json:"inputUrl"`
	Size     int64  `json:"size"`
}

type openResult struct {
	TaskID    string             `json:"taskId"`
	AssetID   string             `json:"assetId"`
	Nonce     string             `json:"nonce"`
	OutputURL string             `json:"outputUrl"`
	Size      int64              `json:"size"`
	Metadata  Metadata           `json:"metadata"`
	Artwork   *artworkDescriptor `json:"artwork,omitempty"`
}

type artworkDescriptor struct {
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 200 {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 || strings.ContainsRune("/\\?#", c) {
			return false
		}
	}
	return true
}

func validNonce(nonce string) bool {
	decoded, err := hex.DecodeString(nonce)
	return err == nil && len(decoded) == 24 && nonce == strings.ToLower(nonce)
}

func decodeRequest(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid NCM request")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid NCM request suffix")
	}
	return nil
}

func cloneMetadata(m Metadata) Metadata {
	if m.AlbumRelease != nil {
		r := *m.AlbumRelease
		m.AlbumRelease = &r
	}
	m.Artists = append([]string(nil), m.Artists...)
	if m.DurationMs != nil {
		v := *m.DurationMs
		m.DurationMs = &v
	}
	if m.BitrateBps != nil {
		v := *m.BitrateBps
		m.BitrateBps = &v
	}
	return m
}

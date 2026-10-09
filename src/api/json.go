package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// DecodeJSON rejects unknown fields, trailing values, and non-JSON bodies.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return domain.Fail("INVALID_ARGUMENT", "Content-Type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid JSON request or unknown field")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return domain.Fail("INVALID_ARGUMENT", "Request must contain one JSON object")
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return domain.Fail("INVALID_ARGUMENT", "Request must contain one JSON object")
	}
	fields := json.NewDecoder(bytes.NewReader(raw))
	fields.DisallowUnknownFields()
	if err := fields.Decode(target); err != nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid JSON request or unknown field")
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(append(encoded, '\n'))
	return err
}

// Package instanceid provides stable UUID identities for plugin instances.
package instanceid

import (
	"strings"

	"github.com/google/uuid"
)

// Valid accepts canonical, non-nil UUIDs used by current configuration and APIs.
func Valid(id string) bool {
	value, err := uuid.Parse(id)
	return err == nil && value != uuid.Nil && value.String() == id
}

// Canonical preserves UUIDs and deterministically upgrades legacy instance IDs.
// It never generates a different identity when configuration is read again.
func Canonical(id string) string {
	if value, err := uuid.Parse(id); err == nil {
		return value.String()
	}
	if id == "" || len(id) > 128 {
		return id
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return id
		}
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("musicfs:plugin-instance:"+strings.ToLower(id))).String()
}

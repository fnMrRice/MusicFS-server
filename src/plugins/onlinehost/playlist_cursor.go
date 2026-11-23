package onlinehost

import (
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func playlistOffset(cursor, version string, limit, size int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	parts := strings.Split(cursor, ".")
	if len(parts) != 3 || len(parts[0]) != 64 || parts[1] != strconv.Itoa(limit) {
		return 0, domain.Fail("INVALID_ARGUMENT", "Invalid playlist cursor")
	}
	if parts[0] != version {
		return 0, domain.Fail("CURSOR_EXPIRED", "Playlist changed; refresh the first page")
	}
	offset, err := strconv.Atoi(parts[2])
	if err != nil || offset < limit || offset >= size || offset%limit != 0 || strconv.Itoa(offset) != parts[2] {
		return 0, domain.Fail("INVALID_ARGUMENT", "Invalid playlist cursor offset")
	}
	return offset, nil
}

func nextPlaylistCursor(version string, limit, end, size int) *string {
	if end >= size {
		return nil
	}
	cursor := version + "." + strconv.Itoa(limit) + "." + strconv.Itoa(end)
	return &cursor
}

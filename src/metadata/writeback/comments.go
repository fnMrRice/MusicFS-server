package writeback

import (
	"bytes"
	"encoding/binary"
	"sort"
	"strings"
	"unicode/utf8"
)

var commentNames = map[string]string{"title": "TITLE", "artists": "ARTIST", "album": "ALBUM", "lyrics": "LYRICS"}

func matchesComment(field, key string) bool {
	return strings.EqualFold(key, commentNames[field]) || (field == "lyrics" && strings.EqualFold(key, "UNSYNCEDLYRICS"))
}

func validatePatch(p Patch) error {
	if len(p.Comments) == 0 && p.Artwork == nil {
		return invalid("No fields were selected")
	}
	bytes := 0
	for field, values := range p.Comments {
		if commentNames[field] == "" || len(values) == 0 || len(values) > 32 || (field != "artists" && len(values) != 1) {
			return invalid("Invalid writeback field or value count")
		}
		for _, value := range values {
			bytes += len(value)
			if strings.TrimSpace(value) == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
				return invalid("Writeback cannot clear or write invalid field values")
			}
		}
	}
	if bytes > 1<<20 || len(p.Artwork) > 8<<20 {
		return invalid("Writeback field values exceed the supported limit")
	}
	if p.Artwork != nil {
		_, err := encodePicture(p.Artwork)
		return err
	}
	return nil
}

func decodeComments(raw []byte) ([]byte, [][]byte, error) {
	readString := func() ([]byte, bool) {
		if len(raw) < 4 {
			return nil, false
		}
		n := uint64(binary.LittleEndian.Uint32(raw))
		raw = raw[4:]
		if n > uint64(len(raw)) {
			return nil, false
		}
		value := raw[:int(n)]
		raw = raw[int(n):]
		return value, utf8.Valid(value)
	}
	vendor, ok := readString()
	if !ok || len(raw) < 4 {
		return nil, nil, unsupported("Invalid FLAC comments")
	}
	count := binary.LittleEndian.Uint32(raw)
	raw = raw[4:]
	if count > 65536 {
		return nil, nil, unsupported("Too many FLAC comments")
	}
	comments := make([][]byte, 0, int(count))
	for range count {
		value, ok := readString()
		name, _, found := bytes.Cut(value, []byte{'='})
		if !ok || !found || len(name) == 0 {
			return nil, nil, unsupported("Invalid FLAC comment field")
		}
		for _, c := range name {
			if c < 32 || c > 126 || c == '=' {
				return nil, nil, unsupported("Invalid FLAC comment field name")
			}
		}
		comments = append(comments, value)
	}
	if len(raw) != 0 {
		return nil, nil, unsupported("Unexpected FLAC comment bytes")
	}
	return vendor, comments, nil
}

func encodeComments(vendor []byte, comments [][]byte) []byte {
	var out bytes.Buffer
	put := func(value []byte) {
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(value)))
		out.Write(value)
	}
	put(vendor)
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(comments)))
	for _, comment := range comments {
		put(comment)
	}
	return out.Bytes()
}

func editBlocks(blocks []flacBlock, p Patch) ([]flacBlock, bool, error) {
	out := make([]flacBlock, 0, len(blocks)+2)
	var comments [][]byte
	vendor := []byte("MusicFS")
	commentIndex := -1
	coverExists := false
	for i, block := range blocks {
		if block.typeID == 4 {
			vendor, comments, _ = decodeComments(block.data)
			commentIndex = i
		}
		if block.typeID == 6 && binary.BigEndian.Uint32(block.data) == 3 {
			coverExists = true
		}
	}
	fields := make([]string, 0, len(p.Comments))
	for field := range p.Comments {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	changedComments := false
	for _, field := range fields {
		name := commentNames[field]
		present := false
		for _, comment := range comments {
			key, value, _ := strings.Cut(string(comment), "=")
			if matchesComment(field, key) && strings.TrimSpace(value) != "" {
				present = true
			}
		}
		if present && !p.Overwrite {
			continue
		}
		retained := make([][]byte, 0, len(comments)+len(p.Comments[field]))
		for _, comment := range comments {
			key, _, _ := strings.Cut(string(comment), "=")
			if !matchesComment(field, key) {
				retained = append(retained, comment)
			}
		}
		for _, value := range p.Comments[field] {
			retained = append(retained, []byte(name+"="+value))
		}
		comments, changedComments = retained, true
	}
	setCover := p.Artwork != nil && (!coverExists || p.Overwrite)
	for _, block := range blocks {
		if block.typeID == 4 && changedComments {
			block.data = encodeComments(vendor, comments)
		}
		if block.typeID == 6 && setCover && binary.BigEndian.Uint32(block.data) == 3 {
			continue
		}
		out = append(out, block)
	}
	if commentIndex == -1 && changedComments {
		out = append(out, flacBlock{4, encodeComments(vendor, comments)})
	}
	if setCover {
		picture, err := encodePicture(p.Artwork)
		if err != nil {
			return nil, false, err
		}
		out = append(out, flacBlock{6, picture})
	}
	return out, changedComments || setCover, nil
}

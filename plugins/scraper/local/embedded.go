package scraperlocal

import (
	"context"
	"fmt"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func (e *Engine) embedded(ctx context.Context, resource enrichment.Resource, reader io.ReaderAt) error {
	header, err := readBounded(ctx, reader, resource.SizeBytes, 0, min(10, resource.SizeBytes), 10)
	if err != nil {
		return err
	}
	if len(header) >= 10 && string(header[:3]) == "ID3" {
		return e.id3(ctx, resource, reader, header)
	}
	if len(header) >= 4 && string(header[:4]) == "fLaC" {
		return e.flac(ctx, resource, reader)
	}
	return fmt.Errorf("UNSUPPORTED: embedded metadata format unavailable")
}

func syncSafe(b []byte) (int, error) {
	if len(b) != 4 {
		return 0, fmt.Errorf("UNSUPPORTED: invalid syncsafe length")
	}
	value := 0
	for _, part := range b {
		if part&0x80 != 0 {
			return 0, fmt.Errorf("UNSUPPORTED: invalid syncsafe integer")
		}
		value = value<<7 | int(part)
	}
	return value, nil
}

func undoUnsynchronization(b []byte) []byte {
	output := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		output = append(output, b[i])
		if b[i] == 0xff && i+1 < len(b) && b[i+1] == 0 {
			i++
		}
	}
	return output
}

package writeback

import (
	"context"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
)

const maxMetadata = 32 << 20

type flacBlock struct {
	typeID byte
	data   []byte
}
type flacFile struct {
	blocks      []flacBlock
	audioOffset int64
}
type EditResult struct {
	AudioSHA512 string
	Changed     bool
}

func readFLAC(r io.ReaderAt, size int64) (flacFile, error) {
	var result flacFile
	var header [4]byte
	if size < 46 {
		return result, unsupported("Source is not a supported native FLAC file")
	}
	if _, err := r.ReadAt(header[:], 0); err != nil || string(header[:]) != "fLaC" {
		return result, unsupported("Source is not a supported native FLAC file")
	}
	offset := int64(4)
	seen := map[byte]bool{}
	for len(result.blocks) < 1024 {
		if _, err := r.ReadAt(header[:], offset); err != nil {
			return result, unsupported("Truncated FLAC metadata")
		}
		kind, length := header[0]&127, int64(header[1])<<16|int64(header[2])<<8|int64(header[3])
		offset += 4
		if kind == 127 || (len(result.blocks) == 0 && kind != 0) || ((kind == 0 || kind == 3 || kind == 4) && seen[kind]) || length > size-offset || offset+length > maxMetadata {
			return result, unsupported("Invalid or excessive FLAC metadata")
		}
		seen[kind] = true
		block := flacBlock{typeID: kind, data: make([]byte, int(length))}
		if _, err := r.ReadAt(block.data, offset); err != nil {
			return result, unsupported("Truncated FLAC metadata")
		}
		if kind == 0 {
			if length != 34 || binary.BigEndian.Uint16(block.data[:2]) < 16 || binary.BigEndian.Uint16(block.data[2:4]) < binary.BigEndian.Uint16(block.data[:2]) || binary.BigEndian.Uint32(block.data[10:14])>>12 == 0 {
				return result, unsupported("Invalid FLAC stream information")
			}
		}
		if kind == 4 {
			if _, _, err := decodeComments(block.data); err != nil {
				return result, err
			}
		}
		if kind == 6 {
			if err := validatePicture(block.data); err != nil {
				return result, err
			}
		}
		result.blocks = append(result.blocks, block)
		offset += length
		if header[0]&128 != 0 {
			if size-offset < 2 {
				return result, unsupported("FLAC audio frames are missing")
			}
			var frame [2]byte
			if _, err := r.ReadAt(frame[:], offset); err != nil || frame[0] != 255 || frame[1]&254 != 248 {
				return result, unsupported("Invalid FLAC audio frame boundary")
			}
			result.audioOffset = offset
			return result, nil
		}
	}
	return result, unsupported("Too many FLAC metadata blocks")
}

// RewriteFLAC copies every audio byte unchanged. It does not decode, re-encode,
// claim to repair invalid audio, or recalculate the stream's existing MD5.
// The destination must be a separate, unpublished file owned by the caller.
func RewriteFLAC(ctx context.Context, src io.ReaderAt, size int64, dst io.Writer, patch Patch) (EditResult, error) {
	var result EditResult
	if err := validatePatch(patch); err != nil {
		return result, err
	}
	file, err := readFLAC(src, size)
	if err != nil {
		return result, err
	}
	blocks, changed, err := editBlocks(file.blocks, patch)
	if err != nil {
		return result, err
	}
	if err = writeFull(dst, []byte("fLaC")); err != nil {
		return result, err
	}
	metadataBytes := 4
	for i, block := range blocks {
		metadataBytes += 4 + len(block.data)
		if len(block.data) > 0xffffff || metadataBytes > maxMetadata {
			return result, unsupported("Edited FLAC metadata exceeds the supported limit")
		}
		header := []byte{block.typeID, byte(len(block.data) >> 16), byte(len(block.data) >> 8), byte(len(block.data))}
		if i == len(blocks)-1 {
			header[0] |= 128
		}
		if err = writeFull(dst, header); err == nil {
			err = writeFull(dst, block.data)
		}
		if err != nil {
			return result, err
		}
	}
	h := sha512.New()
	n, err := copyContext(ctx, io.MultiWriter(dst, h), io.NewSectionReader(src, file.audioOffset, size-file.audioOffset))
	if err != nil {
		return result, err
	}
	if n != size-file.audioOffset {
		return result, io.ErrUnexpectedEOF
	}
	return EditResult{AudioSHA512: fmt.Sprintf("%x", h.Sum(nil)), Changed: changed}, nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := src.Read(buffer)
		if n > 0 {
			if e := writeFull(dst, buffer[:n]); e != nil {
				return total, e
			}
			total += int64(n)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
}

func writeFull(dst io.Writer, data []byte) error {
	n, err := dst.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

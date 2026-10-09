package ncmdecoder

import (
	"bytes"
	"fmt"
	"io"
)

// Reader 的字段初始化后不变；并发读取能力遵循底层 ReaderAt 的契约。
type Reader struct {
	input    io.ReaderAt
	offset   int64
	size     int64
	stream   [256]byte
	metadata Metadata
}

func (r *Reader) Size() int64 { return r.size }

func (r *Reader) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("ACCESS_DENIED: negative audio offset")
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if offset >= r.size {
		return 0, io.EOF
	}
	length := len(dst)
	if int64(length) > r.size-offset {
		length = int(r.size - offset)
	}
	n, err := r.input.ReadAt(dst[:length], r.offset+offset)
	if n < 0 || n > length {
		return 0, fmt.Errorf("TEMPORARY_FAILURE: invalid NCM source read count")
	}
	for i := range n {
		dst[i] ^= r.stream[(uint64(offset)+uint64(i))&255]
	}
	if n < length {
		return n, fmt.Errorf("RESOURCE_CHANGED: NCM source became truncated: %w", io.ErrUnexpectedEOF)
	}
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("TEMPORARY_FAILURE: NCM source read failed")
	}
	if n < len(dst) {
		return n, io.EOF
	}
	return n, nil
}

func (r *Reader) identify() error {
	var header [10]byte
	n, err := r.ReadAt(header[:], 0)
	if err != nil && err != io.EOF {
		return err
	}
	switch {
	case n >= 4 && bytes.Equal(header[:4], []byte("fLaC")):
		r.metadata.Format = "flac"
		r.metadata.MIME = "audio/flac"
	case n >= 10 && bytes.Equal(header[:3], []byte("ID3")):
		if header[3] < 2 || header[3] > 4 || header[4] == 0xff {
			return fmt.Errorf("UNSUPPORTED: invalid decrypted ID3 header")
		}
		var tagSize int64
		for _, value := range header[6:10] {
			if value&0x80 != 0 {
				return fmt.Errorf("UNSUPPORTED: invalid decrypted ID3 size")
			}
			tagSize = tagSize<<7 | int64(value)
		}
		start := int64(10) + tagSize
		if header[3] == 4 && header[5]&0x10 != 0 {
			start += 10
		}
		if start > r.size-4 {
			return fmt.Errorf("UNSUPPORTED: truncated decrypted MP3 tag")
		}
		var frame [4]byte
		if _, err = r.ReadAt(frame[:], start); err != nil {
			return err
		}
		if !mp3Frame(frame[:]) {
			return fmt.Errorf("UNSUPPORTED: decrypted ID3 does not contain MP3 audio")
		}
		r.metadata.Format = "mp3"
		r.metadata.MIME = "audio/mpeg"
	case n >= 4 && mp3Frame(header[:4]):
		r.metadata.Format = "mp3"
		r.metadata.MIME = "audio/mpeg"
	default:
		return fmt.Errorf("UNSUPPORTED: decrypted payload is not MP3 or FLAC")
	}
	return nil
}

func mp3Frame(header []byte) bool {
	return len(header) >= 4 && header[0] == 0xff && header[1]&0xe0 == 0xe0 &&
		header[1]&0x18 != 0x08 && header[1]&0x06 == 0x02 && header[2]&0xf0 != 0xf0 &&
		header[2]&0x0c != 0x0c && header[3]&0x03 != 0x02
}

package ncmdecoder

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	MaxKeyBytes      = 4096
	MaxMetadataBytes = 1 << 20
	MaxCoverBytes    = 64 << 20
)

type containerHeader struct {
	input  io.ReaderAt
	size   int64
	offset int64
}

func (h *containerHeader) read(length int64) ([]byte, error) {
	if length < 0 || h.offset < 0 || length > h.size-h.offset {
		return nil, fmt.Errorf("UNSUPPORTED: truncated NCM header: %w", io.ErrUnexpectedEOF)
	}
	data := make([]byte, int(length))
	if err := readExactAt(h.input, data, h.offset); err != nil {
		return nil, err
	}
	h.offset += length
	return data, nil
}

func (h *containerHeader) length() (int64, error) {
	data, err := h.read(4)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint32(data)), nil
}

func (h *containerHeader) limitedBlock(maximum int64) ([]byte, error) {
	length, err := h.length()
	if err != nil {
		return nil, err
	}
	if length > maximum {
		return nil, fmt.Errorf("UNSUPPORTED: NCM header block exceeds supported limit")
	}
	return h.read(length)
}

func readExactAt(input io.ReaderAt, dst []byte, offset int64) error {
	if len(dst) == 0 {
		return nil
	}
	n, err := input.ReadAt(dst, offset)
	if n != len(dst) {
		return fmt.Errorf("UNSUPPORTED: truncated NCM input: %w", io.ErrUnexpectedEOF)
	}
	if err != nil && err != io.EOF {
		return fmt.Errorf("TEMPORARY_FAILURE: NCM input read failed")
	}
	return nil
}

// NewReader 只持有受限头部和 256 字节密钥流，不读取或缓存完整音频。
func NewReader(input io.ReaderAt, size int64) (*Reader, error) {
	if input == nil || size < 10 {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NCM input")
	}
	h := containerHeader{input: input, size: size}
	header, err := h.read(10)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(header[:8], []byte("CTENFDAM")) {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NCM signature")
	}
	// 两个版本/保留字节由已知参考实现跳过，未假定其含义。
	key, err := h.limitedBlock(MaxKeyBytes)
	if err != nil {
		return nil, err
	}
	stream, err := deriveStream(key)
	if err != nil {
		return nil, err
	}
	metadataBlock, err := h.limitedBlock(MaxMetadataBytes)
	if err != nil {
		return nil, err
	}
	metadata, err := decodeMetadata(metadataBlock)
	clear(metadataBlock)
	if err != nil {
		return nil, err
	}
	// CRC/保留区共五字节；首个封面长度是容量，第二个是有效图像长度。
	if _, err = h.read(5); err != nil {
		return nil, err
	}
	coverCapacity, err := h.length()
	if err != nil {
		return nil, err
	}
	coverLength, err := h.length()
	if err != nil {
		return nil, err
	}
	if coverCapacity > MaxCoverBytes || coverLength > coverCapacity {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NCM cover bounds")
	}
	if coverCapacity > size-h.offset {
		return nil, fmt.Errorf("UNSUPPORTED: truncated NCM cover: %w", io.ErrUnexpectedEOF)
	}
	coverOffset := h.offset
	h.offset += coverCapacity
	if size-h.offset < 4 {
		return nil, fmt.Errorf("UNSUPPORTED: missing NCM audio payload")
	}
	r := &Reader{input: input, offset: h.offset, size: size - h.offset, stream: stream, metadata: metadata, artworkOffset: coverOffset, artworkSize: coverLength}
	var last [1]byte
	if err = readExactAt(input, last[:], size-1); err != nil {
		return nil, err
	}
	if err = r.identify(); err != nil {
		return nil, err
	}
	return r, nil
}

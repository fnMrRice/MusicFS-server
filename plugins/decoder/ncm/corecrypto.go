package ncmdecoder

import (
	"bytes"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/internal/neteasetag"
)

// 此固定值属于 NCM 格式，不是用户凭据；布局参考见 REFERENCES.md。
var keyWrappingKey = [16]byte{0x68, 0x7a, 0x48, 0x52, 0x41, 0x6d, 0x73, 0x6f, 0x35, 0x6b, 0x49, 0x6e, 0x62, 0x61, 0x78, 0x57}

func deriveStream(encrypted []byte) ([256]byte, error) {
	var stream [256]byte
	for i := range encrypted {
		encrypted[i] ^= 0x64
	}
	plain, err := neteasetag.DecryptBlocks(encrypted, keyWrappingKey)
	clear(encrypted)
	if err != nil {
		return stream, err
	}
	defer clear(plain)
	prefix := []byte("neteasecloudmusic")
	if !bytes.HasPrefix(plain, prefix) || len(plain) <= len(prefix) {
		return stream, fmt.Errorf("UNSUPPORTED: invalid NCM audio key")
	}
	key := plain[len(prefix):]
	var permutation [256]byte
	for i := range permutation {
		permutation[i] = byte(i)
	}
	position := 0
	for i := range permutation {
		position = (position + int(permutation[i]) + int(key[i%len(key)])) & 255
		permutation[i], permutation[position] = permutation[position], permutation[i]
	}
	for i := range stream {
		index := (i + 1) & 255
		first := int(permutation[index])
		second := int(permutation[(first+index)&255])
		stream[i] = permutation[(first+second)&255]
	}
	clear(permutation[:])
	return stream, nil
}

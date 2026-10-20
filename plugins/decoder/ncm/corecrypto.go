package ncmdecoder

import (
	"bytes"
	"crypto/aes"
	"fmt"
)

// 这两个固定值属于 NCM 格式，不是用户凭据；布局参考见 REFERENCES.md。
var keyWrappingKey = [16]byte{0x68, 0x7a, 0x48, 0x52, 0x41, 0x6d, 0x73, 0x6f, 0x35, 0x6b, 0x49, 0x6e, 0x62, 0x61, 0x78, 0x57}
var metadataWrappingKey = [16]byte{0x23, 0x31, 0x34, 0x6c, 0x6a, 0x6b, 0x5f, 0x21, 0x5c, 0x5d, 0x26, 0x30, 0x55, 0x3c, 0x27, 0x28}

func decryptBlocks(data []byte, key [16]byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NCM encrypted block size")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NCM cipher configuration")
	}
	plain := make([]byte, len(data))
	for start := 0; start < len(data); start += aes.BlockSize {
		block.Decrypt(plain[start:start+aes.BlockSize], data[start:start+aes.BlockSize])
	}
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > aes.BlockSize || padding > len(plain) {
		clear(plain)
		return nil, fmt.Errorf("UNSUPPORTED: invalid NCM block padding")
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			clear(plain)
			return nil, fmt.Errorf("UNSUPPORTED: invalid NCM block padding")
		}
	}
	return plain[:len(plain)-padding], nil
}

func deriveStream(encrypted []byte) ([256]byte, error) {
	var stream [256]byte
	for i := range encrypted {
		encrypted[i] ^= 0x64
	}
	plain, err := decryptBlocks(encrypted, keyWrappingKey)
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

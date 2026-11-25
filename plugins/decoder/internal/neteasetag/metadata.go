// Package neteasetag 解析网易云写入 NCM 头和普通音频注释的同一种元数据封装。
package neteasetag

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
)

const Prefix = "163 key(Don't modify):"

var metadataKey = [16]byte{0x23, 0x31, 0x34, 0x6c, 0x6a, 0x6b, 0x5f, 0x21, 0x5c, 0x5d, 0x26, 0x30, 0x55, 0x3c, 0x27, 0x28}

func SongID(fields map[string]json.RawMessage) string {
	return decimalID(fields["musicId"])
}

func decimalID(raw json.RawMessage) string {
	var value json.Number
	if json.Unmarshal(raw, &value) != nil || !audiotypes.ValidNetEaseID(value.String()) {
		return ""
	}
	return value.String()
}

// Release 要求发行与歌曲编号来自同一原始标签，并与本次读取到的曲名和艺人相符。
func Release(fields map[string]json.RawMessage, title, album string, artists []string) *audiotypes.AlbumRelease {
	if SongID(fields) == "" || !audiotypes.SameAlbumTrack(title, album, artists, Text(fields["musicName"]), Text(fields["album"]), Artists(fields)) {
		return nil
	}
	id := decimalID(fields["albumId"])
	if id == "" {
		return nil
	}
	return &audiotypes.AlbumRelease{Namespace: "netease", ID: id}
}

func Text(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil || len(text) > 4096 {
		return ""
	}
	return text
}

func Artists(fields map[string]json.RawMessage) []string {
	var names []string
	var artists []json.RawMessage
	if json.Unmarshal(fields["artist"], &artists) == nil && len(artists) <= 256 {
		for _, artist := range artists {
			var pair []json.RawMessage
			name := ""
			if json.Unmarshal(artist, &pair) == nil && len(pair) > 0 {
				name = Text(pair[0])
			} else {
				name = Text(artist)
			}
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func Parse(data []byte) (map[string]json.RawMessage, error) {
	if len(data) > 1<<20 || !bytes.HasPrefix(data, []byte(Prefix)) {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NetEase metadata tag")
	}
	encoded := data[len(Prefix):]
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Strict().Decode(decoded, encoded)
	if err != nil {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NetEase metadata encoding")
	}
	plain, err := DecryptBlocks(decoded[:n], metadataKey)
	clear(decoded)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	body, ok := bytes.CutPrefix(plain, []byte("music:"))
	var fields map[string]json.RawMessage
	if !ok || !utf8.Valid(body) || json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, fmt.Errorf("UNSUPPORTED: invalid NetEase music metadata")
	}
	return fields, nil
}

// DecryptBlocks 读取此格式带 PKCS#7 填充的块，用于元数据和 NCM 的音频密钥。
func DecryptBlocks(data []byte, key [16]byte) ([]byte, error) {
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

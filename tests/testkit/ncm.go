package testkit

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// NCMOptions 控制合成测试容器；不代表真实客户端生成的文件。
type NCMOptions struct {
	Cover        []byte
	CoverPadding int
	Key          []byte
}

// BuildNCM 从明文音频构造独立的合成 NCM fixture，nil metadata 表示缺失。
func BuildNCM(payload []byte, metadata any) ([]byte, error) {
	return BuildNCMWithOptions(payload, metadata, NCMOptions{})
}

// BuildNCMWithOptions 只用于测试；实现不调用生产解密代码。
func BuildNCMWithOptions(payload []byte, metadata any, options NCMOptions) ([]byte, error) {
	if options.CoverPadding < 0 || options.CoverPadding > 64<<20 || len(options.Cover) > (64<<20)-options.CoverPadding {
		return nil, fmt.Errorf("invalid synthetic NCM cover capacity")
	}
	seed := options.Key
	if seed == nil {
		seed = []byte("musicfs-independent-fixture-seed")
	}
	if len(seed) == 0 || len(seed) > 4000 {
		return nil, fmt.Errorf("invalid synthetic NCM key length")
	}
	keyPlain := append([]byte("neteasecloudmusic"), seed...)
	keyBlock := fixtureECB(keyPlain, []byte("hzHRAmso5kInbaxW"))
	for i := range keyBlock {
		keyBlock[i] ^= 0x64
	}
	var metaBlock []byte
	if metadata != nil {
		body, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		metaKey := []byte{0x23, 0x31, 0x34, 0x6c, 0x6a, 0x6b, 0x5f, 0x21, 0x5c, 0x5d, 0x26, 0x30, 0x55, 0x3c, 0x27, 0x28}
		ciphertext := fixtureECB(append([]byte("music:"), body...), metaKey)
		metaBlock = []byte("163 key(Don't modify):" + base64.StdEncoding.EncodeToString(ciphertext))
		for i := range metaBlock {
			metaBlock[i] ^= 0x63
		}
	}
	var out bytes.Buffer
	out.WriteString("CTENFDAM")
	out.Write([]byte{0, 0})
	writeFixtureBlock(&out, keyBlock)
	writeFixtureBlock(&out, metaBlock)
	out.Write(make([]byte, 5))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(options.Cover)+options.CoverPadding))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(options.Cover)))
	out.Write(options.Cover)
	out.Write(make([]byte, options.CoverPadding))
	box := [256]byte{}
	for i := range box {
		box[i] = byte(i)
	}
	var previous byte
	for i := range box {
		previous += box[i] + seed[i%len(seed)]
		box[i], box[previous] = box[previous], box[i]
	}
	for index, sample := range payload {
		position := byte(index + 1)
		first := box[position]
		second := box[first+position]
		out.WriteByte(sample ^ box[first+second])
	}
	return out.Bytes(), nil
}

func writeFixtureBlock(out *bytes.Buffer, data []byte) {
	_ = binary.Write(out, binary.LittleEndian, uint32(len(data)))
	out.Write(data)
}

func fixtureECB(plain, key []byte) []byte {
	block, _ := aes.NewCipher(key)
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	padded := make([]byte, len(plain)+padding)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(padded[i:], padded[i:])
	}
	return padded
}

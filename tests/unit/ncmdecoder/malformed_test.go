package ncmdecoder_test

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
)

// 这些测试重新构造损坏的加密区块，不依赖生产实现的解密函数。
func encryptedPlain(plain []byte, key string, pad bool) []byte {
	copyPlain := append([]byte(nil), plain...)
	if pad {
		padding := 16 - len(copyPlain)%16
		copyPlain = append(copyPlain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	}
	block, _ := aes.NewCipher([]byte(key))
	for i := 0; i < len(copyPlain); i += 16 {
		block.Encrypt(copyPlain[i:i+16], copyPlain[i:i+16])
	}
	return copyPlain
}

func replaceBlock(data []byte, offset int, block []byte) []byte {
	end := offset + 4 + int(binary.LittleEndian.Uint32(data[offset:]))
	out := append([]byte(nil), data[:offset]...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(block)))
	out = append(out, block...)
	return append(out, data[end:]...)
}

func xor(data []byte, value byte) []byte {
	for i := range data {
		data[i] ^= value
	}
	return data
}

func metaBlock(plain []byte, pad bool) []byte {
	ciphertext := encryptedPlain(plain, "#14ljk_!\\]&0U<'(", pad)
	return xor([]byte("163 key(Don't modify):"+base64.StdEncoding.EncodeToString(ciphertext)), 0x63)
}

func TestMalformedHeadersAndBlocks(t *testing.T) {
	valid := fixture(t, payloadFor("mp3", 200), nil)
	metaOffset := 14 + int(binary.LittleEndian.Uint32(valid[10:]))
	coverOffset := metaOffset + 4 + 5
	cases := map[string][]byte{}
	mutate := func(name string, offset int, value uint32) {
		data := append([]byte(nil), valid...)
		binary.LittleEndian.PutUint32(data[offset:], value)
		cases[name] = data
	}
	mutate("huge key", 10, ^uint32(0))
	mutate("huge metadata", metaOffset, ^uint32(0))
	mutate("huge cover", coverOffset, ^uint32(0))
	mutate("cover actual exceeds capacity", coverOffset+4, 1)
	mutate("truncated cover", coverOffset, 300)
	cases["empty key"] = replaceBlock(valid, 10, nil)
	cases["nonblock key"] = replaceBlock(valid, 10, []byte{1, 2, 3})
	cases["wrong key prefix"] = replaceBlock(valid, 10, xor(encryptedPlain([]byte("wrong-key"), "hzHRAmso5kInbaxW", true), 0x64))
	cases["empty key seed"] = replaceBlock(valid, 10, xor(encryptedPlain([]byte("neteasecloudmusic"), "hzHRAmso5kInbaxW", true), 0x64))
	for _, padding := range []byte{0, 17, 255} {
		plain := bytes.Repeat([]byte{padding}, 32)
		cases["bad padding "+string(rune(padding))] = replaceBlock(valid, 10, xor(encryptedPlain(plain, "hzHRAmso5kInbaxW", false), 0x64))
	}
	plain := bytes.Repeat([]byte{2}, 32)
	plain[30] = 3
	cases["inconsistent padding"] = replaceBlock(valid, 10, xor(encryptedPlain(plain, "hzHRAmso5kInbaxW", false), 0x64))
	cases["metadata prefix"] = replaceBlock(valid, metaOffset, xor([]byte("wrong"), 0x63))
	cases["metadata base64"] = replaceBlock(valid, metaOffset, xor([]byte("163 key(Don't modify):!!!!"), 0x63))
	cases["metadata short AES"] = replaceBlock(valid, metaOffset, xor([]byte("163 key(Don't modify):AQID"), 0x63))
	cases["metadata padding"] = replaceBlock(valid, metaOffset, metaBlock(make([]byte, 16), false))
	cases["metadata envelope"] = replaceBlock(valid, metaOffset, metaBlock([]byte("other:{}"), true))
	cases["metadata JSON"] = replaceBlock(valid, metaOffset, metaBlock([]byte("music:{"), true))
	cases["metadata null"] = replaceBlock(valid, metaOffset, metaBlock([]byte("music:null"), true))
	cases["metadata array"] = replaceBlock(valid, metaOffset, metaBlock([]byte("music:[]"), true))
	cases["metadata UTF8"] = replaceBlock(valid, metaOffset, metaBlock([]byte{'m', 'u', 's', 'i', 'c', ':', '{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}, true))
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ncmdecoder.NewReader(bytes.NewReader(data), int64(len(data)))
			if err == nil || !strings.Contains(err.Error(), "UNSUPPORTED") {
				t.Fatalf("invalid data accepted or unsafe error: %v", err)
			}
		})
	}
	for end := 0; end < coverOffset+8+4; end++ {
		if _, err := ncmdecoder.NewReader(bytes.NewReader(valid[:end]), int64(end)); err == nil {
			t.Fatalf("truncated header accepted at %d", end)
		}
	}
	if _, err := ncmdecoder.NewReader(bytes.NewReader(valid[:len(valid)-1]), int64(len(valid))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("advertised size not checked: %v", err)
	}
}

func TestUnsupportedPayload(t *testing.T) {
	for _, payload := range [][]byte{nil, []byte("fLa"), []byte("OggSbad"), {0xff, 0xf1, 0x90, 0, 0, 0}, []byte("ID3bad"), {'I', 'D', '3', 3, 0, 0, 0x80, 0, 0, 0}, {'I', 'D', '3', 3, 0, 0, 0, 0, 0, 100}, {'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0, 'f', 'L', 'a', 'C'}} {
		data := fixture(t, payload, nil)
		if _, err := ncmdecoder.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
			t.Fatalf("unsupported payload accepted: %x", payload)
		}
	}
	if _, err := ncmdecoder.NewReader(nil, 100); err == nil {
		t.Fatal("nil accepted")
	}
	if _, err := ncmdecoder.NewReader(bytes.NewReader(nil), -1); err == nil {
		t.Fatal("negative size accepted")
	}
	data := fixture(t, payloadFor("mp3", 40), nil)
	data[0] = 'X'
	if _, err := ncmdecoder.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("bad signature accepted")
	}
}

type changingInput struct {
	data *bytes.Reader
	mode int
}

func (s *changingInput) ReadAt(dst []byte, offset int64) (int, error) {
	switch s.mode {
	case 1:
		return s.data.ReadAt(dst[:len(dst)/2], offset)
	case 2:
		return 0, errors.New("private source secret")
	case 3:
		return len(dst) + 1, nil
	default:
		return s.data.ReadAt(dst, offset)
	}
}

func TestSourceShortReadsAndErrors(t *testing.T) {
	data := fixture(t, payloadFor("flac", 400), nil)
	source := &changingInput{data: bytes.NewReader(data)}
	r, err := ncmdecoder.NewReader(source, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	source.mode = 1
	if _, err = r.ReadAt(make([]byte, 30), 20); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short read accepted: %v", err)
	}
	if _, err = ncmdecoder.NewReader(source, int64(len(data))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short header accepted: %v", err)
	}
	for _, mode := range []int{2, 3} {
		source.mode = mode
		if _, err = r.ReadAt(make([]byte, 30), 20); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe source error: %v", err)
		}
	}
}

func FuzzMalformedNCM(f *testing.F) {
	f.Add([]byte("CTENFDAM"))
	valid := fixture(f, payloadFor("mp3", 100), nil)
	f.Add(valid)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 2<<20 {
			t.Skip()
		}
		r, err := ncmdecoder.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		if r.Size() <= 0 || r.Size() > int64(len(data)) {
			t.Fatal("invalid payload size")
		}
		var dst [519]byte
		_, _ = r.ReadAt(dst[:], r.Size()/2)
	})
}

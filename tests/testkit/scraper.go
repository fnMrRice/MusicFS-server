package testkit

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

// These synthetic metadata fixtures contain no third-party song or artwork.
func ScraperPNG() []byte {
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	var output bytes.Buffer
	_ = png.Encode(&output, picture)
	return output.Bytes()
}

func ScraperJPEG() []byte {
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{B: 255, A: 255})
	var output bytes.Buffer
	_ = jpeg.Encode(&output, picture, &jpeg.Options{Quality: 90})
	return output.Bytes()
}

type ScraperFrame struct {
	ID    string
	Flags uint16
	Body  []byte
}

func ScraperID3(version byte, frames ...ScraperFrame) []byte {
	var body bytes.Buffer
	for _, frame := range frames {
		header := make([]byte, 10)
		copy(header, frame.ID)
		if version == 4 {
			putSyncSafe(header[4:8], len(frame.Body))
		} else {
			binary.BigEndian.PutUint32(header[4:8], uint32(len(frame.Body)))
		}
		binary.BigEndian.PutUint16(header[8:], frame.Flags)
		body.Write(header)
		body.Write(frame.Body)
	}
	header := []byte{'I', 'D', '3', version, 0, 0, 0, 0, 0, 0}
	putSyncSafe(header[6:], body.Len())
	return append(header, body.Bytes()...)
}

func putSyncSafe(b []byte, value int) {
	for i := 3; i >= 0; i-- {
		b[i] = byte(value & 127)
		value >>= 7
	}
}

type ScraperBlock struct {
	Kind byte
	Body []byte
}

func ScraperFLAC(blocks ...ScraperBlock) []byte {
	all := append([]ScraperBlock{{Kind: 0, Body: make([]byte, 34)}}, blocks...)
	output := []byte("fLaC")
	for i, block := range all {
		kind := block.Kind
		if i == len(all)-1 {
			kind |= 0x80
		}
		n := len(block.Body)
		output = append(output, kind, byte(n>>16), byte(n>>8), byte(n))
		output = append(output, block.Body...)
	}
	return output
}

func ScraperVorbis(comments ...string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(comments)))
	for _, comment := range comments {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(comment)))
		b.WriteString(comment)
	}
	return b.Bytes()
}

func ScraperFLACPicture(picture []byte, mime string) []byte {
	var b bytes.Buffer
	for _, value := range []uint32{3, uint32(len(mime))} {
		_ = binary.Write(&b, binary.BigEndian, value)
	}
	b.WriteString(mime)
	for _, value := range []uint32{0, 2, 2, 32, 0, uint32(len(picture))} {
		_ = binary.Write(&b, binary.BigEndian, value)
	}
	b.Write(picture)
	return b.Bytes()
}

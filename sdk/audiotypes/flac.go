package audiotypes

import "encoding/binary"

// FLACStreamInfo 读取首个 STREAMINFO 块中的采样率、声道及采样总数。
func FLACStreamInfo(header []byte) (rate, channels int, samples int64, ok bool) {
	if len(header) < 42 || string(header[:4]) != "fLaC" || header[4]&0x7f != 0 || binary.BigEndian.Uint32(header[4:8])&0xffffff != 34 {
		return
	}
	info := header[8:42]
	minBlock, maxBlock := binary.BigEndian.Uint16(info), binary.BigEndian.Uint16(info[2:])
	minFrame := binary.BigEndian.Uint32(info[3:7]) & 0xffffff
	maxFrame := binary.BigEndian.Uint32(info[6:10]) & 0xffffff
	props := binary.BigEndian.Uint64(info[10:])
	rate, channels = int(props>>44), int(props>>41&7)+1
	bits := int(props>>36&31) + 1
	ok = minBlock >= 16 && maxBlock >= minBlock && (minFrame == 0 || maxFrame == 0 || maxFrame >= minFrame) && rate > 0 && bits >= 4
	return rate, channels, int64(props & ((1 << 36) - 1)), ok
}

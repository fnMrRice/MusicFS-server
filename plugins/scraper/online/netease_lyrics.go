package online

import (
	"encoding/json"
	"regexp"
	"strings"
)

const neteaseInstrumentalNotice = "纯音乐，请欣赏"

var neteaseLRCMetadata = regexp.MustCompile(`(?i)^(?:\[(?:ar|al|ti|au|by|offset|length|re|ve):[^\]]*\]\s*)+$`)
var neteaseLyricCredit = regexp.MustCompile(`^(?:作词|作曲|编曲|制作人)\s*[:：]`)

// 仅当整份正文都是纯音乐提示时跳过候选。其他歌词按上游原文交付。
func neteaseInstrumentalLyrics(lyrics string) bool {
	found := false
	for raw := range strings.SplitSeq(strings.TrimPrefix(lyrics, "\ufeff"), "\n") {
		line := strings.TrimSpace(raw)
		for stamp := lyricTimestamp.FindString(line); stamp != ""; stamp = lyricTimestamp.FindString(line) {
			line = strings.TrimSpace(line[len(stamp):])
		}
		if line == "" || neteaseLRCMetadata.MatchString(line) {
			continue
		}
		if strings.HasPrefix(line, "{") {
			line = neteaseLyricSegments(line)
		}
		if neteaseLyricCredit.MatchString(line) {
			continue
		}
		if line != neteaseInstrumentalNotice {
			return false
		}
		found = true
	}
	return found
}

// 网易云署名带毫秒时间和文本片段；无法识别的行仍按正文处理。
func neteaseLyricSegments(line string) string {
	var row struct {
		T *int64
		C []struct{ Tx *string }
	}
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		input := strings.ReplaceAll(strings.TrimSuffix(line, `\`), `\:`, ":")
		if input == line || json.Unmarshal([]byte(input), &row) != nil {
			return line
		}
	}
	if row.T == nil || *row.T < 0 || len(row.C) == 0 {
		return line
	}
	var text strings.Builder
	for _, part := range row.C {
		if part.Tx == nil {
			return line
		}
		text.WriteString(*part.Tx)
	}
	return strings.TrimSpace(text.String())
}

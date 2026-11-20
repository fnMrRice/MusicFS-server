package online

import (
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// 仅使用平台实际给出的名字和语言；没有语言标记时保留 und。
func addPlatformName(names []scraper.LocalizedTitle, primary, locale, name string) []scraper.LocalizedTitle {
	if locale == "" {
		locale = "und"
	}
	if len(names) >= scraper.MaxLocalizedTitles || name == "" || locale == "und" && strings.EqualFold(strings.TrimSpace(name), primary) {
		return names
	}
	normalized, err := scraper.NormalizeLocalizedTitles([]scraper.LocalizedTitle{{Language: locale, Title: name}})
	if err != nil {
		return names
	}
	entry := normalized[0]
	for _, saved := range names {
		if saved.Language == entry.Language && strings.EqualFold(saved.Title, entry.Title) {
			return names
		}
	}
	return append(names, entry)
}

func numericTrackID(id int64) string {
	if id > 0 {
		return strconv.FormatInt(id, 10)
	}
	return ""
}

package scraper

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

// LocalizedTitle 表示实际提供的名称；未知语言使用 und，同一语言可有多个别名。
type LocalizedTitle struct {
	Language string `json:"language"`
	Title    string `json:"title"`
}

const MaxLocalizedTitles = 64

// NormalizeLocalizedTitles 统一语言标签并按语言、名称去重，保留声明顺序。
// nil 表示未提供字段；显式提交空数组可清除已保存的别名。
func NormalizeLocalizedTitles(titles []LocalizedTitle) ([]LocalizedTitle, error) {
	if titles == nil {
		return nil, nil
	}
	if len(titles) > MaxLocalizedTitles {
		return nil, errors.New("too many localized names")
	}
	result := make([]LocalizedTitle, 0, len(titles))
	seen := make(map[LocalizedTitle]bool, len(titles))
	for _, entry := range titles {
		tag := strings.TrimSpace(entry.Language)
		entry.Title = strings.TrimSpace(entry.Title)
		if tag == "" || len(tag) > 63 || strings.Contains(tag, "_") || entry.Title == "" || !utf8.ValidString(entry.Title) || utf8.RuneCountInString(entry.Title) > 256 || strings.ContainsFunc(entry.Title, unicode.IsControl) {
			return nil, errors.New("invalid localized name")
		}
		parsed, err := language.Parse(tag)
		if err != nil {
			return nil, errors.New("invalid localized name language")
		}
		entry.Language = parsed.String()
		key := LocalizedTitle{Language: entry.Language, Title: strings.ToLower(entry.Title)}
		if !seen[key] {
			seen[key] = true
			result = append(result, entry)
		}
	}
	return result, nil
}

// Package scraper 定义在线资料来源的自描述契约，宿主负责选择和保存结果。
package scraper

import "context"

type Descriptor struct {
	PluginID string   `json:"pluginId"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Fields   []string `json:"fields"`
	// 仅在所有字段使用同一作品编号时声明，避免将艺人、录音或发行编号混用。
	ExternalIDNamespace string   `json:"externalIdNamespace,omitempty"`
	RequiresExternalID  bool     `json:"requiresExternalId,omitempty"`
	ImageHosts          []string `json:"imageHosts,omitempty"`
	Available           bool     `json:"available"`
}

// Query 的时长单位为秒；外部编号由选中的插件和字段解释。
type Query struct {
	Title       string  `json:"title,omitempty"`
	Artist      string  `json:"artist,omitempty"`
	Album       string  `json:"album,omitempty"`
	ExternalID  string  `json:"externalId,omitempty"`
	Country     string  `json:"country,omitempty"`
	Fingerprint string  `json:"fingerprint,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
}

type Track struct {
	Title    string  `json:"title"`
	Artist   string  `json:"artist,omitempty"`
	Disc     int     `json:"disc,omitempty"`
	Number   int     `json:"number,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

// Item 使用上游稳定编号；曲目表保留来源顺序，图片保存来源引用。
type Item struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist,omitempty"`
	Album       string  `json:"album,omitempty"`
	Description string  `json:"description,omitempty"`
	Date        string  `json:"date,omitempty"`
	SourceURL   string  `json:"sourceUrl,omitempty"`
	ImageURL    string  `json:"imageUrl,omitempty"`
	Lyrics      string  `json:"lyrics,omitempty"`
	Format      string  `json:"format,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
	Tracks      []Track `json:"tracks,omitempty"`
}

type Plugin interface {
	Descriptor() Descriptor
	Search(context.Context, string, Query) ([]Item, error)
}

// ValidField 限定可独立选择、保存和替换的资料槽位。
func ValidField(field string) bool {
	switch field {
	case "albumDetails", "albumTracks", "artistDetails", "artistImage", "trackDetails", "recordingIdentity", "artwork", "lyrics", "workDetails":
		return true
	}
	return false
}

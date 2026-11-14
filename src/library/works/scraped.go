package works

import "github.com/fnMrRice/MusicFS-server/sdk/scraper"

// ScrapedField 单独记录人工选择的资料、上游编号和出处，保留原手填资料。
type ScrapedField struct {
	Field    string       `json:"field"`
	PluginID string       `json:"pluginId"`
	Item     scraper.Item `json:"item"`
	// 仅供选用请求在存储 RPC 上传输，保存到作品前清除。
	CandidateToken string `json:"candidateToken,omitempty"`
}

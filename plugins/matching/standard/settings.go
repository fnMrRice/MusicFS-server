package standard

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"strings"
)

const PluginID = "musicfs.plugin.matching.standard"

func Detector() matching.Detector {
	return matching.Detector{ID: PluginID, Schema: Settings(), Buckets: Buckets, Automatic: func(values settings.Values) bool { enabled, _ := values["automatic"].(bool); return enabled }, Scorer: func(values settings.Values) candidates.Scorer {
		minimum := float64(35)
		if value, ok := values["minimumScore"].(float64); ok {
			minimum = value
		}
		return configuredScorer{minimum}
	}}
}

type configuredScorer struct{ minimum float64 }

func (s configuredScorer) Find(ctx context.Context, anchor candidates.Input, others []candidates.Input) ([]candidates.Candidate, error) {
	items, err := (Plugin{}).Find(ctx, anchor, others)
	if err != nil {
		return nil, err
	}
	filtered := []candidates.Candidate{}
	for _, c := range items {
		if float64(c.Score) >= s.minimum {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

func Settings() pluginsettings.Schema {
	minimum, maximum := float64(1), float64(100)
	return pluginsettings.Schema{Revision: "matching-settings-v1", Title: "重复识别", Description: "发现可能属于同一录音的来源，保留全部文件，由你确认是否合并。", LocalizedTitles: map[string]string{"en": "Duplicate detection"}, LocalizedDescriptions: map[string]string{"en": "Find sources that may belong to the same recording. All files are kept and every merge needs your confirmation."}, Fields: []pluginsettings.Field{
		{Key: "automatic", Type: "checkbox", Label: "自动识别重复歌曲", Description: "曲库或音频证据变化后自动检查。", LocalizedLabels: map[string]string{"en": "Detect duplicates automatically"}, LocalizedDescriptions: map[string]string{"en": "Check after library or audio evidence changes."}, DefaultValue: true},
		{Key: "minimumScore", Type: "number", Label: "最低匹配分数", Description: "35 包含歌名、歌手和时长一致的候选；提高分数可仅保留更强的音频证据。分数不是概率。保存后在下一次识别时使用；关闭自动识别时，请点‘立即识别’。", LocalizedLabels: map[string]string{"en": "Minimum matching score"}, LocalizedDescriptions: map[string]string{"en": "35 includes matching titles, artists and duration. Higher scores require stronger audio evidence. Scores are not probabilities. Saved settings apply to the next detection. When automatic detection is off, select Detect now."}, DefaultValue: float64(35), Min: &minimum, Max: &maximum},
	}, Actions: []pluginsettings.Action{{ID: "scan", Label: "立即识别", Description: "重新检查全部曲目，不会自动合并或删除文件。", LocalizedLabels: map[string]string{"en": "Detect now"}, LocalizedDescriptions: map[string]string{"en": "Check all tracks again. Nothing is merged or deleted automatically."}}}}
}

// Buckets 由插件挑选需要比较的候选；宿主仍检查每一对的权限与人工决定。
// 指纹桶允许不同标签的录音比较，标题桶与文件哈希桶避免普通曲库的全量两两计算。
func Buckets(source matching.CandidateSource) []string {
	keys := []string{}
	if title := strings.ToLower(strings.Join(strings.Fields(source.Input.Title), " ")); title != "" {
		keys = append(keys, "title:"+title)
	}
	if source.Input.FileHash != nil {
		keys = append(keys, "hash:"+source.Input.FileHash.SHA512)
	}
	for _, f := range source.Fingerprints {
		if f.Current && f.Quality.Status == "available" {
			keys = append(keys, "fingerprint")
			break
		}
	}
	return keys
}

package pluginproto

import "github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"

// QualityOptions 是插件接受的单次下载音质，返回媒体的实际编码和码率仍由平台决定。
type QualityOptions struct {
	DefaultQuality string                  `json:"defaultQuality"`
	Options        []pluginsettings.Option `json:"options"`
}

func (q QualityOptions) Supports(quality string) bool {
	for _, option := range q.Options {
		if option.Value == quality {
			return true
		}
	}
	return false
}

func (q *QualityOptions) validate() error {
	if q == nil {
		return nil
	}
	if len(q.Options) > 32 || !q.Supports(q.DefaultQuality) {
		return Failure("PROTOCOL_MISMATCH", "Invalid default download quality")
	}
	seen := map[string]bool{}
	for _, option := range q.Options {
		if !validAudioTag(option.Value) || seen[option.Value] || !onlineDisplayName(option.Label) || len(option.LocalizedLabels) > 32 {
			return Failure("PROTOCOL_MISMATCH", "Invalid download quality option")
		}
		seen[option.Value] = true
		for tag, label := range option.LocalizedLabels {
			if len(tag) > 63 || !onlineLanguageTag.MatchString(tag) || !onlineDisplayName(label) {
				return Failure("PROTOCOL_MISMATCH", "Invalid localized quality label")
			}
		}
	}
	return nil
}

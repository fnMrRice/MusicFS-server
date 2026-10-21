package localfiles

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func (w *worker) displayCount(raw json.RawMessage) (*plugindisplay.Count, error) {
	var request pluginproto.DisplayCountRequest
	if pluginproto.DecodeOnline(raw, &request) != nil || request.InstanceID != w.instance || request.RootIDs == nil || len(request.RootIDs) > 1024 {
		return nil, pluginproto.Failure("ACCESS_DENIED", "Invalid display count scope")
	}
	seen := map[string]bool{}
	for _, id := range request.RootIDs {
		if id == "" || len(id) > 128 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.ContainsFunc(id, unicode.IsControl) || seen[id] {
			return nil, pluginproto.Failure("ACCESS_DENIED", "Invalid display count root identity")
		}
		seen[id] = true
	}
	return &plugindisplay.Count{Value: int64(len(seen)), Unit: "sources", LocalizedUnits: map[string]string{
		"zh": "个来源", "zh-Hans": "个来源", "zh-Hant": "個來源", "ja": "ソース",
	}}, nil
}

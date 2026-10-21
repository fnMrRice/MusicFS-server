package localfiles

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestDisplayCountUsesSelectedRootIdentities(t *testing.T) {
	w := &worker{ready: true, instance: "local-instance"}
	for _, roots := range [][]string{{}, {"music", "archive"}} {
		raw, _ := json.Marshal(pluginproto.DisplayCountRequest{InstanceID: w.instance, RootIDs: roots})
		result, err := w.handle(context.Background(), "plugin.displayCount", raw)
		if err != nil {
			t.Fatal(err)
		}
		count, ok := result.(*plugindisplay.Count)
		if !ok || count.Validate() != nil || count.Value != int64(len(roots)) || count.Unit != "sources" || count.LocalizedUnits["zh-Hans"] != "个来源" {
			t.Fatalf("unexpected plugin statistic: %#v", result)
		}
	}
	for _, request := range []string{
		`{"instanceId":"other","rootIds":["music"]}`,
		`{"instanceId":"local-instance","rootIds":["music","music"]}`,
		`{"instanceId":"local-instance","rootIds":null}`,
		`{"instanceId":"local-instance","rootIds":[""]}`,
		`{"instanceId":"local-instance","rootIds":["music"],"path":"private"}`,
	} {
		if _, err := w.handle(context.Background(), "plugin.displayCount", json.RawMessage(request)); err == nil {
			t.Fatal("invalid scope or root identities were accepted", request)
		}
	}
}

package plugins_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"golang.org/x/sys/windows"
)

func TestLocalRebindRecoversDurablePartialMapping(t *testing.T) {
	w, cfg, r := rebindFixture(t)
	ctx := context.Background()
	name, _ := windows.UTF16PtrFromString(filepath.Join(cfg.StateDir, "plugin-state", "track-"+r.ExternalID+".json"))
	// Hold the second publication target without delete sharing. The intent and
	// new alias are persisted, then replacement of this exact file must fail.
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.RebindLocalTrack(ctx, r)
	windows.CloseHandle(h)
	if err == nil {
		t.Fatal("expected a partial mapping publication failure")
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err = w.Scan(ctx, scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "blocked", Generation: 2, Limit: 200})); err == nil {
		t.Fatal("restart scanned partial mapping")
	}
	if _, err = w.Resolve(ctx, pluginproto.ResolveRequest{ExternalID: r.ExternalID, ExpectedVersion: r.OldVersion}); err == nil {
		t.Fatal("restart resolved partial mapping")
	}
	if _, err = w.RebindLocalTrack(ctx, r); err != nil {
		t.Fatal("mapping recovery failed", err)
	}
	page, err := w.Scan(ctx, scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "recovered", Generation: 3, Limit: 200}))
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != r.ExternalID {
		t.Fatalf("recovery changed source identity: %+v %v", page, err)
	}
}

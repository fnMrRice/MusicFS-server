package pluginhost

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestScanReceiptRejectsUnobservedTracksAndUsesHostExclusions(t *testing.T) {
	entry := pluginproto.Entry{RootID: "root", RelativePath: "normal.wav", ObservedVersion: "v1", SizeBytes: 10}
	receipt := &scanReceipt{page: pluginproto.EntriesPage{Items: []pluginproto.Entry{entry}, RootIdentity: "identity"}, excluded: []string{"private"}}
	w := &Worker{scanGrants: map[string]scanGrant{"grant": {receipt: receipt}}}
	newPage := func() pluginproto.ScanPage {
		return pluginproto.ScanPage{RootIdentity: "identity", Complete: true, Items: []pluginproto.Observation{{RootID: entry.RootID, RelativePath: entry.RelativePath, ObservedVersion: entry.ObservedVersion, SizeBytes: entry.SizeBytes}}}
	}
	for name, mutate := range map[string]func(*pluginproto.ScanPage){
		"hidden-path":    func(p *pluginproto.ScanPage) { p.Items[0].RelativePath = "private/hidden.wav" },
		"wrong-root":     func(p *pluginproto.ScanPage) { p.Items[0].RootID = "outside" },
		"new-version":    func(p *pluginproto.ScanPage) { p.Items[0].ObservedVersion = "v2" },
		"missing-entry":  func(p *pluginproto.ScanPage) { p.Items = nil },
		"extra-entry":    func(p *pluginproto.ScanPage) { p.Items = append(p.Items, p.Items[0]) },
		"wrong-cursor":   func(p *pluginproto.ScanPage) { p.NextCursor = "invented" },
		"wrong-identity": func(p *pluginproto.ScanPage) { p.RootIdentity = "other" },
		"partial":        func(p *pluginproto.ScanPage) { p.Complete = false },
	} {
		t.Run(name, func(t *testing.T) {
			page := newPage()
			mutate(&page)
			if w.validateScanReceipt("grant", &page) == nil {
				t.Fatal("untrusted scan page accepted")
			}
		})
	}
	page := newPage()
	if err := json.Unmarshal([]byte(`{"excludedPaths":["."]}`), &page); err != nil {
		t.Fatal(err)
	}
	if err := w.validateScanReceipt("grant", &page); err != nil || len(page.ExcludedPaths) != 1 || page.ExcludedPaths[0] != "private" {
		t.Fatal("provider replaced host exclusions", err)
	}
	page.ExcludedPaths[0] = "mutated"
	if receipt.excluded[0] != "private" {
		t.Fatal("public result changed host snapshot")
	}
	if w.validateScanReceipt("missing", &page) == nil {
		t.Fatal("scan without host enumeration accepted")
	}
}

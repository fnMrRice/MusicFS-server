package pluginhost_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestScanReceiptRejectsUnobservedTracksAndUsesHostExclusions(t *testing.T) {
	modes := []string{"hidden-path", "wrong-root", "new-version", "missing-entry", "extra-entry", "wrong-cursor", "wrong-identity", "partial", "missing-receipt", "valid"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			cfg := localConfig(t, mode)
			root := cfg.Roots[0].Path
			if err := os.WriteFile(filepath.Join(root, "normal.wav"), []byte("audio"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "private"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "private", "hidden.wav"), []byte("private audio"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "private", ".nomedia"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			w := startLocal(t, cfg)
			identity := ""
			for _, r := range w.Access.Roots() {
				if r.ID == "one" {
					identity = r.ExpectedIdentity
				}
			}
			request := pluginproto.ScanRequest{RootID: "one", ScanID: "scan", Generation: 1, Limit: 100, PlanDigest: strings.Repeat("a", 64), Scope: pluginproto.ScanScope{RootID: "one", RootIdentity: identity, Recursive: true, RespectNoMedia: true}}
			page, err := w.Scan(t.Context(), request)
			if mode != "valid" {
				if err == nil {
					t.Fatal("untrusted scan page accepted")
				}
				return
			}
			if err != nil || len(page.Items) != 1 || page.Items[0].RelativePath != "normal.wav" || len(page.ExcludedPaths) != 1 || page.ExcludedPaths[0] != "private" {
				t.Fatal("provider replaced host exclusions", err, page)
			}
			page.ExcludedPaths[0] = "mutated"
			again, err := w.Scan(t.Context(), request)
			if err != nil || len(again.ExcludedPaths) != 1 || again.ExcludedPaths[0] != "private" {
				t.Fatal("public result changed host evidence", err)
			}
		})
	}
}

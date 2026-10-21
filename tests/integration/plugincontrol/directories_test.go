package plugincontrol_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func directoryManager(t *testing.T, paths []string) *plugincontrol.Manager {
	t.Helper()
	i := config.Instance{InstanceID: localInstanceID, Name: "Local", Enabled: true, PendingConfiguration: true, Permissions: &config.Permissions{ReadDirectories: paths}}
	m, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Enabled: true, Instances: []config.Instance{i}}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func TestDirectoryHTTPPendingRootsPermissionsAndQueries(t *testing.T) {
	music := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	child := filepath.Join(music, "Collection")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "private-song.wav"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	m := directoryManager(t, []string{music, missing})
	server, cookies := managementHTTP(t, m)
	const route = "/admin/plugin-instances/" + localInstanceID + "/directories"
	for role, status := range map[string]int{"": 401, "user": 403} {
		requireHTTP(t, managementRequest(server, cookies[role], "GET", route, "", "", ""), status, "")
	}
	w := managementRequest(server, cookies["admin"], "GET", route, "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "ManagedPluginDirectories", w.Body.Bytes())
	var page plugincontrol.Directories
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Path != nil || page.ParentPath != nil || len(page.Items) != 2 || !page.Items[0].Available || page.Items[1].Available || page.Items[1].Path != missing {
		t.Fatalf("invalid allowed roots: %s", w.Body.String())
	}
	w = managementRequest(server, cookies["admin"], "GET", route+"?path="+url.QueryEscape(music), "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "ManagedPluginDirectories", w.Body.Bytes())
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.Path == nil || *page.Path != music || page.ParentPath != nil || len(page.Items) != 1 || page.Items[0].Path != child || !page.Items[0].Available || strings.Contains(w.Body.String(), "private-song") {
		t.Fatalf("invalid child directory response: %s", w.Body.String())
	}
	w = managementRequest(server, cookies["admin"], "GET", route+"?path="+url.QueryEscape(child), "", "", "")
	requireHTTP(t, w, 200, "")
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if page.ParentPath == nil || *page.ParentPath != music || len(page.Items) != 0 {
		t.Fatalf("invalid parent boundary: %s", w.Body.String())
	}
	for _, query := range []string{"?unknown=1", "?path=", "?cursor=", "?path=a&path=b", "?cursor=a&cursor=b", "?path=%GG", "?path=relative", "?path=" + url.QueryEscape(music+string(filepath.Separator)+".."), "?cursor=forged", "?cursor=" + strings.Repeat("x", 257), "?path=" + url.QueryEscape(music+"\x00")} {
		requireHTTP(t, managementRequest(server, cookies["admin"], "GET", route+query, "", "", ""), 400, "INVALID_ARGUMENT")
	}
	requireHTTP(t, managementRequest(server, cookies["admin"], "GET", route+"?path="+url.QueryEscape(filepath.Dir(music)), "", "", ""), 403, "ACCESS_DENIED")
	requireHTTP(t, managementRequest(server, cookies["admin"], "GET", route+"?path="+url.QueryEscape(missing), "", "", ""), 503, "SOURCE_UNAVAILABLE")
	if runtime.GOOS == "windows" {
		for _, path := range []string{`\\?\C:\Music`, `\\.\C:\Music`, `C:Music`, music + `\CON`, music + `\name.`, music + `\name `, music + `\name:stream`, music + `\*.wav`} {
			requireHTTP(t, managementRequest(server, cookies["admin"], "GET", route+"?path="+url.QueryEscape(path), "", "", ""), 400, "INVALID_ARGUMENT")
		}
	}
	projection, err := m.Configuration(localInstanceID)
	if err != nil || len(projection.Roots) != 0 {
		t.Fatal("browsing changed selected roots", err)
	}
	link := filepath.Join(music, "escape")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("directory symlink creation unavailable: %v", err)
	}
	requireHTTP(t, managementRequest(server, cookies["admin"], "GET", route+"?path="+url.QueryEscape(link), "", "", ""), 503, "SOURCE_UNAVAILABLE")
}

func TestDirectoryPaginationAndCursorScope(t *testing.T) {
	music := t.TempDir()
	for i := 0; i < 215; i++ {
		if err := os.Mkdir(filepath.Join(music, fmt.Sprintf("collection-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	m := directoryManager(t, []string{music})
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("pagination did not finish")
		}
		page, err := m.Directories(context.Background(), localInstanceID, music, cursor)
		if err != nil || len(page.Items) > 100 {
			t.Fatalf("invalid directory page: %+v %v", page, err)
		}
		for _, item := range page.Items {
			if seen[item.Path] {
				t.Fatal("duplicate directory across pages")
			}
			seen[item.Path] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if len(seen) != 215 {
		t.Fatal("pagination lost directories", len(seen))
	}
	_, err := m.Directories(context.Background(), localInstanceID, "", cursor)
	code(t, err, "CURSOR_EXPIRED")
	if err := os.Mkdir(filepath.Join(music, "added"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = m.Directories(context.Background(), localInstanceID, music, cursor)
	code(t, err, "CURSOR_EXPIRED")
}

func TestDirectoryHTTPSelectionPersistsChildWithoutExpandingAuthority(t *testing.T) {
	m, allowed := configurationManager(t)
	selected := filepath.Join(allowed, "Collection")
	if err := os.MkdirAll(selected, 0700); err != nil {
		t.Fatal(err)
	}
	server, cookies := managementHTTP(t, m)
	const route = "/admin/plugin-instances/" + localInstanceID + "/configuration"
	body, _ := json.Marshal(map[string]any{"musicRoots": []config.MusicRoot{{ID: "collection", Name: "Collection", Path: selected}}})
	w := managementRequest(server, cookies["admin"], "PUT", route, string(body), m.Snapshot().Revision, "select-child")
	requireHTTP(t, w, 202, "")
	_, instance, err := m.InstanceConfig(localInstanceID)
	if err != nil || len(instance.Config.MusicRoots) != 1 || instance.Config.MusicRoots[0].Path != selected {
		t.Fatal("child directory was not selected", err)
	}
	if boundary, err := config.RootBoundary(instance, selected); err != nil || boundary != allowed {
		t.Fatal("selection changed deployment authority", boundary, err)
	}
	w = managementRequest(server, cookies["admin"], "GET", route, "", "", "")
	requireHTTP(t, w, 200, "")
	if strings.Contains(w.Body.String(), "private-music-root") {
		t.Fatal("configuration projection leaked absolute paths")
	}
	page, err := m.Directories(context.Background(), localInstanceID, allowed, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ConfiguredRootID == nil || *page.Items[0].ConfiguredRootID != "collection" || page.ConfiguredRootID != nil {
		t.Fatal("selected child identity missing", page, err)
	}
	page, err = m.Directories(context.Background(), localInstanceID, selected, "")
	if err != nil || page.ConfiguredRootID == nil || *page.ConfiguredRootID != "collection" {
		t.Fatal("current directory identity missing", page, err)
	}
	duplicate := selected
	if runtime.GOOS == "windows" {
		duplicate = strings.ToUpper(selected)
	}
	body, _ = json.Marshal(map[string]any{"musicRoots": []config.MusicRoot{{ID: "collection", Name: "Collection", Path: selected}, {ID: "duplicate", Name: "Duplicate", Path: duplicate}}})
	w = managementRequest(server, cookies["admin"], "PUT", route, string(body), m.Snapshot().Revision, "duplicate-child")
	requireHTTP(t, w, 400, "INVALID_ARGUMENT")
}

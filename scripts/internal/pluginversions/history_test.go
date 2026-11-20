package pluginversions

import (
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdatesFollowPluginHistory(t *testing.T) {
	root := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(label string) {
		t.Helper()
		run(root, "add", ".")
		run(root, "commit", "-qm", label)
	}
	expect := func(dir string, want map[string]string) {
		t.Helper()
		got, err := Resolve(dir)
		if err != nil || !maps.Equal(got, want) {
			t.Fatalf("got %v, %v; want %v", got, err, want)
		}
	}
	run(root, "init", "-q", "-b", "main")
	run(root, "config", "user.name", "Build Test")
	run(root, "config", "user.email", "test@example.invalid")
	run(root, "config", "commit.gpgSign", "false")
	run(root, "config", "core.hooksPath", filepath.Join(root, "empty-hooks"))
	write("README.md", "before declarations")
	commit("before")
	declarations := `{"groups":{"shared":["shared/"]},"plugins":{"one":{"version":"0.4.2","sources":["one/","@shared"]},"two":{"version":"1.1.0","sources":["two/","@shared"]}}}`
	write(manifestPath, declarations)
	write("plugins/one/main.go", "one")
	write("plugins/two/main.go", "two")
	initial := map[string]string{"one": "0.4.2", "two": "1.1.0"}
	expect(root, initial)
	commit("baseline")
	write("plugins/one/main_test.go", "tests only")
	write("plugins/one/README.md", "documentation only")
	write("plugins/one/tests/fixture.go", "fixture only")
	commit("tests and docs")
	expect(root, initial)
	write("plugins/one/new.go", "untracked")
	write("plugins/one/main.go", "modified")
	run(root, "add", "plugins/one/main.go")
	oneChanged := map[string]string{"one": "0.5.0", "two": "1.1.0"}
	expect(root, oneChanged)
	expect(root, oneChanged)
	commit("one plugin")
	expect(root, oneChanged)
	run(root, "checkout", "-qb", "feature")
	write("plugins/shared/implementation.go", "shared")
	commit("shared implementation")
	write("plugins/shared/implementation.go", "shared update")
	commit("more shared changes")
	expect(root, map[string]string{"one": "0.7.0", "two": "1.3.0"})
	run(root, "checkout", "-q", "main")
	run(root, "merge", "--no-ff", "-qm", "merge feature", "feature")
	merged := map[string]string{"one": "0.6.0", "two": "1.2.0"}
	expect(root, merged)
	withThird := strings.Replace(declarations, `"two":{`, `"three":{"version":"0.1.0","sources":["three/"]},"two":{`, 1)
	write(manifestPath, withThird)
	write("plugins/three/main.go", "new plugin")
	commit("add plugin")
	merged["three"] = "0.1.0"
	expect(root, merged)
	clone := filepath.Join(t.TempDir(), "fresh")
	run(root, "clone", "-q", "--local", "--no-hardlinks", root, clone)
	expect(clone, merged)
	// 在克隆副本中标记浅历史边界，等价于 depth=1，测试无需远程传输进程。
	if err := os.WriteFile(filepath.Join(clone, ".git", "shallow"), []byte(run(clone, "rev-parse", "HEAD")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(clone); err == nil {
		t.Fatal("incomplete release history accepted")
	}
	write("plugins/shared-new/unknown.go", "new implementation")
	if _, err := Resolve(root); err == nil || !strings.Contains(err.Error(), "unknown.go") {
		t.Fatalf("missing plugin owner was not reported: %v", err)
	}
	commit("source added before owner declaration")
	write(manifestPath, strings.Replace(withThird, `"one/"`, `"one/","shared-new/"`, 1))
	commit("declare new source owner")
	merged["one"] = "0.7.0"
	expect(root, merged)
}

func TestSourceDeclarationsCoverPluginImplementations(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	releases, err := parseCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	err = fs.WalkDir(os.DirFS(root), "plugins", func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, ok := implementation(file)
		if !ok {
			return nil
		}
		for _, p := range releases.Plugins {
			if p.owns(name) {
				return nil
			}
		}
		t.Errorf("plugin source has no release declaration: %s", file)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

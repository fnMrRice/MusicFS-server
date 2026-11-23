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
	declarations := `{"groups":{"shared":["shared/"]},"plugins":{"one":{"version":"0.4.2","sources":["one/","@shared"]},"two":{"version":"0.6.0","sources":["two/","@shared"]}}}`
	write(manifestPath, declarations)
	write("plugins/one/main.go", "one")
	write("plugins/two/main.go", "two")
	initial := map[string]string{"one": "0.4.2", "two": "0.6.0"}
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
	oneChanged := map[string]string{"one": "0.5.0", "two": "0.6.0"}
	expect(root, oneChanged)
	expect(root, oneChanged)
	commit("one plugin")
	expect(root, oneChanged)
	run(root, "checkout", "-qb", "feature")
	write("plugins/shared/implementation.go", "shared")
	commit("shared implementation")
	write("plugins/shared/implementation.go", "shared update")
	commit("more shared changes")
	expect(root, map[string]string{"one": "0.7.0", "two": "0.8.0"})
	run(root, "checkout", "-q", "main")
	run(root, "merge", "--no-ff", "-qm", "merge feature", "feature")
	merged := map[string]string{"one": "0.6.0", "two": "0.7.0"}
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
	withOwners := strings.Replace(withThird, `"one/"`, `"one/","shared-new/"`, 1)
	write(manifestPath, withOwners)
	commit("declare new source owner")
	merged["one"] = "0.7.0"
	expect(root, merged)

	// 切换提交本身采用新策略。已经按原规则发布的版本继续作为计数起点。
	write(manifestPath, strings.Replace(withOwners, "{", `{"versionPolicy":"conventional-commits",`, 1))
	write("plugins/one/main.go", "first fix")
	expect(root, merged)
	commit("fix(one): correct first issue")
	for _, step := range []struct {
		file, message, one, two string
	}{
		{"plugins/one/main.go", "fix(one): correct second issue", "0.7.2", "0.7.0"},
		{"plugins/one/main.go", "feat(one): add an option", "0.8.0", "0.7.0"},
		{"plugins/shared/implementation.go", "perf(shared): speed up parsing", "0.8.1", "0.7.1"},
		{"plugins/shared/implementation.go", "refactor(shared): simplify parsing", "0.8.2", "0.7.2"},
		{"plugins/one/main.go", "docs(one): update examples", "0.8.2", "0.7.2"},
		{"plugins/one/main_test.go", "feat(one): add test fixtures", "0.8.2", "0.7.2"},
		{"plugins/one/main.go", "fix(one)!: replace the format", "0.9.0", "0.7.2"},
		{"plugins/two/main.go", "chore(two): revise input\n\nBREAKING CHANGES: old inputs are unsupported", "0.9.0", "0.8.0"},
		{"plugins/one/main.go", "revert(one): restore the format", "0.9.1", "0.8.0"},
	} {
		write(step.file, step.message)
		commit(step.message)
		expect(root, map[string]string{"one": step.one, "two": step.two, "three": "0.1.0"})
	}
	run(root, "checkout", "-qb", "second-feature")
	write("plugins/one/main.go", "feature")
	commit("feat(one): add a feature")
	write("plugins/one/main.go", "improved feature")
	commit("fix(one): repair the feature")
	run(root, "checkout", "-q", "main")
	run(root, "merge", "--no-ff", "-qm", "feat(one): integrate feature", "second-feature")
	merged = map[string]string{"one": "0.10.0", "two": "0.8.0", "three": "0.1.0"}
	expect(root, merged)
	run(root, "checkout", "-qb", "mixed")
	write("plugins/shared/implementation.go", "shared fix")
	commit("fix(shared): handle empty input")
	run(root, "checkout", "-qb", "nested-command")
	write("plugins/three/main.go", "new command")
	commit("fix(three): replace the command")
	nested := run(root, "rev-parse", "HEAD")
	run(root, "checkout", "-q", "mixed")
	run(root, "merge", "--no-ff", "-qm", "refactor(three)!: replace command line", "nested-command")
	run(root, "checkout", "-qb", "nested-parser")
	write("plugins/shared/implementation.go", "updated shared fix")
	commit("feat(shared): enable alternate parser")
	run(root, "checkout", "-q", "mixed")
	run(root, "merge", "--no-ff", "-qm", "fix(shared): integrate parser correction", "nested-parser")
	write("plugins/one/main.go", "discarded feature")
	commit("feat(one): try another format")
	write("plugins/one/main.go", "improved feature")
	commit("revert(one): discard experimental format")
	run(root, "checkout", "-q", "main")
	run(root, "merge", "--no-ff", "--no-edit", "mixed")
	merged = map[string]string{"one": "0.10.1", "two": "0.8.1", "three": "0.2.0"}
	expect(root, merged)
	write("plugins/shared/implementation.go", "pending")
	run(root, "add", "plugins/shared/implementation.go")
	write("plugins/one/untracked.go", "untracked")
	expect(root, merged)
	expect(root, merged)
	if err := os.WriteFile(filepath.Join(root, ".git", "shallow"), []byte(nested+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete merge side branch accepted: %v", err)
	}
}

func TestClassifyCommit(t *testing.T) {
	for message, want := range map[string]increment{
		"feat: implement search": minor, "fix!: replace old names": minor,
		"refactor(core): change contract\n\nBREAKING CHANGE: renamed field": minor,
		"chore: change wire format\n\nBREAKING-CHANGE: renamed field":       minor,
		"fix: repair\n\nA BREAKING CHANGE: example in prose":                patch,
		"revert: restore a prior change":                                    patch, "build: update tooling": unchanged,
		"fix spelling": unchanged, "": unchanged,
		"FEAT(one): support searching":                 minor,
		"chore: switch\n\nBREAKING CHANGE:renamed key": minor,
		`Revert "feat(one): add search"`:               patch,
	} {
		if got := classifyCommit(message); got != want {
			t.Errorf("%q: got %v, want %v", message, got, want)
		}
	}
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

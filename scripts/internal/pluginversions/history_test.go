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

func TestReleaseBatches(t *testing.T) {
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
	commit := func(subject string) {
		t.Helper()
		run(root, "add", ".")
		run(root, "commit", "--allow-empty", "-qm", subject)
	}
	run(root, "init", "-q", "-b", "main")
	run(root, "config", "user.name", "Build Test")
	run(root, "config", "user.email", "test@example.invalid")
	run(root, "config", "commit.gpgSign", "false")
	run(root, "config", "core.hooksPath", filepath.Join(root, "empty-hooks"))
	declarations := `{"versionPolicy":"conventional-commits","groups":{"shared":["shared/"]},"plugins":{"one":{"version":"0.1.0","sources":["one/","@shared"]},"two":{"version":"0.1.0","sources":["two/","@shared"]}}}`
	write(manifestPath, declarations)
	write("plugins/one/main.go", "one")
	write("plugins/two/main.go", "two")
	write("plugins/shared/implementation.go", "shared")
	commit("previously published release")
	base := run(root, "rev-parse", "HEAD")
	published := map[string]string{"one": "0.7.2", "two": "0.4.8"}
	expect := func(at string, want map[string]string) {
		t.Helper()
		got, err := resolve(at, base, published)
		if err != nil || !maps.Equal(got, want) {
			t.Fatalf("got %v, %v; want %v", got, err, want)
		}
	}
	expect(root, published)
	write("plugins/one/main.go", "first fix")
	commit("fix(one): repair search")
	oneFix := map[string]string{"one": "0.7.3", "two": "0.4.8"}
	expect(root, oneFix)
	commit("chore(release): publish v0.01.0")
	write("plugins/one/main.go", "second fix")
	commit("perf(one): accelerate search")
	expect(root, oneFix)
	write("plugins/two/main_test.go", "test only")
	write("plugins/two/README.md", "documentation only")
	commit("feat(two): add fixtures")
	expect(root, oneFix)
	write("plugins/one/main.go", "a new feature")
	commit("feat(one): add a command")
	write("plugins/shared/implementation.go", "discarded feature")
	commit("feat(shared): try a protocol")
	write("plugins/shared/implementation.go", "shared")
	commit("revert(shared): discard the protocol")
	write("plugins/two/main.go", "bug fix")
	commit("fix(two): repair decoding")
	withThird := strings.Replace(declarations, `"two":{`, `"three":{"version":"0.1.0","sources":["three/"]},"two":{`, 1)
	write(manifestPath, strings.Replace(withThird, "conventional-commits", "release-batches", 1))
	write("plugins/three/main.go", "new plugin")
	commit("feat(three): add a plugin")
	write("plugins/three/main.go", "ready for release")
	commit("fix(three): validate input")
	first := map[string]string{"one": "0.8.0", "two": "0.5.0", "three": "0.1.0"}
	expect(root, first)
	write(serverVersionPath, `package version
const Server = "0.20.0"`)
	commit("chore(release): publish v0.20.0")
	expect(root, first)
	expect(root, first)
	// 第二批有多个修复，均以首批已发值为起点。
	write("plugins/shared/implementation.go", "shared correction")
	commit("fix(shared): repair decoding")
	write("plugins/shared/implementation.go", "refined correction")
	write(serverVersionPath, `package version
const Server = "0.20.1"`)
	commit("refactor(shared): simplify decoding")
	second := map[string]string{"one": "0.8.1", "two": "0.5.1", "three": "0.1.0"}
	expect(root, second)
	commit("chore(release): publish v0.20.1")
	expect(root, second)
	// 侧支功能在合入前撤回，默认合并相对第一父没有实现文件差异。
	run(root, "checkout", "-qb", "discarded")
	write("plugins/two/main.go", "temporary feature")
	commit("feat(two): add an experimental decoder")
	write("plugins/two/main.go", "bug fix")
	commit("revert(two): drop the experiment")
	run(root, "checkout", "-q", "main")
	run(root, "merge", "--no-ff", "--no-edit", "discarded")
	expect(root, second)
	// 侧支的发布标题不结束主线批次。嵌套的显式合并类型优先于其侧支提交类型。
	run(root, "checkout", "-qb", "mixed")
	write("plugins/shared/implementation.go", "upgrade")
	commit("fix(shared): handle new values")
	run(root, "checkout", "-qb", "nested")
	write("plugins/one/main.go", "replacement")
	commit("fix(one): replace the command")
	nested := run(root, "rev-parse", "HEAD")
	run(root, "checkout", "-q", "mixed")
	run(root, "merge", "--no-ff", "-qm", "refactor(one)!: replace the command", "nested")
	commit("chore(release): publish v0.22.0")
	write("plugins/shared/implementation.go", "final shared fix")
	commit("fix(shared): address another issue")
	run(root, "checkout", "-q", "main")
	run(root, "merge", "--no-ff", "--no-edit", "mixed")
	third := map[string]string{"one": "0.9.0", "two": "0.5.2", "three": "0.1.0"}
	expect(root, third)
	write("plugins/one/main.go", "dirty")
	run(root, "add", "plugins/one/main.go")
	write("plugins/one/untracked.go", "untracked")
	expect(root, third)
	expect(root, third)
	write("plugins/unowned/main.go", "untracked without owner")
	if _, err := resolve(root, base, published); err == nil || !strings.Contains(err.Error(), "unowned") {
		t.Fatalf("missing source owner not reported: %v", err)
	}
	clone := filepath.Join(t.TempDir(), "fresh")
	run(root, "clone", "-q", "--local", "--no-hardlinks", root, clone)
	expect(clone, third)
	run(clone, "config", "user.name", "Build Test")
	run(clone, "config", "user.email", "test@example.invalid")
	run(clone, "checkout", "-qb", "bad-release")
	run(clone, "-c", "commit.gpgSign=false", "commit", "--allow-empty", "-qm", "chore(release): publish v0.20.2")
	if _, err := resolve(clone, base, published); err == nil || !strings.Contains(err.Error(), "requires const Server") {
		t.Fatalf("mismatched server release accepted: %v", err)
	}
	run(clone, "checkout", "-q", "main")
	if _, err := resolve(clone, nested, published); err == nil || !strings.Contains(err.Error(), "first-parent") {
		t.Fatalf("side-branch baseline accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, ".git", "shallow"), []byte(base+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	expect(clone, third)
	if err := os.WriteFile(filepath.Join(clone, ".git", "shallow"), []byte(nested+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(clone, base, published); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete merge side branch accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(clone, ".git", "shallow"), []byte(run(clone, "rev-parse", "HEAD")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(clone, base, published); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete first-parent release history accepted: %v", err)
	}
}

func TestPublicationTitle(t *testing.T) {
	for _, subject := range []string{"chore(release): publish v0.0.0", "chore(release): publish v0.19.200"} {
		if !releaseTitle.MatchString(subject) {
			t.Errorf("valid release title rejected: %q", subject)
		}
	}
	for _, subject := range []string{
		"chore(release): publish v1.2.3", "chore(release): publish v0.00.2", "chore(release): publish v0.2.03",
		"chore(release): publish v0.2.3-rc", "chore(release): publish v0.2.3 ", "CHORE(release): publish v0.2.3",
		"chore(release): publish  v0.2.3", "prepare chore(release): publish v0.2.3",
	} {
		if releaseTitle.MatchString(subject) {
			t.Errorf("invalid release title accepted: %q", subject)
		}
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

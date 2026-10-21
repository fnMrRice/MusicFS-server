package e2e_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestInvalidSelectionDoesNotPolluteProtocolOutput(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "musicfs-plugins.exe")
	build := exec.Command("go", "build", "-mod=readonly", "-o", binary, "./src/app/musicfs-plugins")
	build.Dir = testkit.RepositoryRoot()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build unified plugin: %v: %s", err, out)
	}
	for _, args := range [][]string{nil, {"--plugin", "unknown"}, {"--plugin", "local", "extra"}, {"--plugin=netease", "--plugin=qqmusic"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binary, args...)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
				t.Fatalf("expected argument exit status 2, got %v", err)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatal("argument errors must only write to stderr")
			}
		})
	}
}

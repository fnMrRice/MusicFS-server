package dspworker_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

var fixtureBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "musicfs-dsp-fixture-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixtureBinary = filepath.Join(dir, "dsp-worker.exe")
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", fixtureBinary, "./tests/testapps/dsp-worker")
	cmd.Dir = testkit.RepositoryRoot()
	output, err := cmd.CombinedOutput()
	code := 1
	if err != nil {
		fmt.Fprintf(os.Stderr, "Build DSP fixture: %v: %s\n", err, output)
	} else {
		code = m.Run()
	}
	os.RemoveAll(dir)
	os.Exit(code)
}

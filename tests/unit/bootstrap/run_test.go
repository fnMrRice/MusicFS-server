package bootstrap_test

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/version"
)

func TestCompositionBoundary(t *testing.T) {
	var output bytes.Buffer
	if err := bootstrap.Run([]string{"--version"}, &output); err != nil {
		t.Fatal(err)
	}
	if want := "MusicFS Server " + version.Server + " (API v" + bootstrap.APIVersion + ")\n"; output.String() != want {
		t.Fatalf("version output = %q, want %q", output.String(), want)
	}
}

func TestCLIHelpNamesYAMLConfiguration(t *testing.T) {
	var output bytes.Buffer
	if err := bootstrap.Run([]string{"serve", "--help"}, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("unexpected help result: %v", err)
	}
	if !strings.Contains(output.String(), "Configuration YAML path (.yaml or .yml)") || strings.Contains(output.String(), "Configuration JSON") {
		t.Fatalf("configuration help was not migrated: %s", output.String())
	}
}

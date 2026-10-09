package bootstrap_test

import (
	"bytes"
	"errors"
	"flag"
	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
	"strings"
	"testing"
)

func TestCompositionBoundary(t *testing.T) {
	var output bytes.Buffer
	if err := bootstrap.Run([]string{"--version"}, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("missing startup output")
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

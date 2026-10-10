package bootstrap_test

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
)

func TestLifecycleCLIExposesOwnershipButNotUnsafeTiming(t *testing.T) {
	var output bytes.Buffer
	if err := bootstrap.Run([]string{"serve", "--help"}, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	help := output.String()
	if !strings.Contains(help, "startup-mode") || !strings.Contains(help, "standalone") || strings.Contains(help, "lease-ttl") || strings.Contains(help, "shutdown-timeout") {
		t.Fatal("unexpected lifecycle flags", help)
	}
	if err := bootstrap.Run([]string{"serve", "--startup-mode", "takeover"}, &output); err == nil || !strings.Contains(err.Error(), "Startup mode") {
		t.Fatal("invalid startup ownership accepted", err)
	}
}

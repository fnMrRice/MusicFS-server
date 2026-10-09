package bootstrap_test

import (
	"bytes"
	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
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

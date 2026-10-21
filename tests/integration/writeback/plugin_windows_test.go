package writeback_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/tags/flac"
	"github.com/fnMrRice/MusicFS-server/sdk/tagformat"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
)

type corruptingFormat struct {
	tagformat.Format
	rewritten bool
}

func (p *corruptingFormat) Rewrite(ctx context.Context, src io.ReaderAt, size int64, dst io.Writer, patch tagformat.Patch) (tagformat.EditResult, error) {
	var output bytes.Buffer
	result, err := p.Format.Rewrite(ctx, src, size, &output, patch)
	if err != nil {
		return result, err
	}
	body := output.Bytes()
	body[len(body)-1] ^= 0xff
	p.rewritten = true
	_, err = dst.Write(body)
	return result, err
}

func TestHostRejectsTagPluginAudioChangesBeforeReplacingSource(t *testing.T) {
	root, original, policy, request, host, journal := setup(t)
	plugin := &corruptingFormat{Format: flac.New()}
	service, err := writeback.New(policy, journal, host, plugin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Apply(context.Background(), "admin", request); err == nil || !plugin.rewritten {
		t.Fatalf("changed audio was not rejected at the plugin boundary: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, request.Target.RelativePath))
	if err != nil || !bytes.Equal(after, original) || host.commits != 0 {
		t.Fatal("plugin audio corruption replaced the source")
	}
}

func TestWritebackRequiresExplicitTagPlugin(t *testing.T) {
	_, _, policy, _, host, journal := setup(t)
	if _, err := writeback.New(policy, journal, host, nil); err == nil {
		t.Fatal("writeback accepted a missing tag format plugin")
	}
}

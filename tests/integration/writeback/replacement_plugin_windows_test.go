package writeback_test

import (
	"bytes"
	"context"
	"crypto/sha512"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/tagformat"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
)

// replacementTagFormat 使用独立的合成容器，验证宿主不再要求 FLAC 扩展名或字节。
type replacementTagFormat struct{}

func (replacementTagFormat) Descriptor() tagformat.Descriptor {
	return tagformat.Descriptor{Formats: []string{"fixture"}, Extensions: []string{".fixture"}, Fields: []string{"title", "artists", "album", "lyrics", "artwork"}}
}
func (replacementTagFormat) ValidatePatch(tagformat.Patch) error { return nil }
func (replacementTagFormat) AudioRange(_ io.ReaderAt, size int64) (tagformat.AudioRange, error) {
	if size < 8 {
		return tagformat.AudioRange{}, fmt.Errorf("UNSUPPORTED: invalid fixture container")
	}
	return tagformat.AudioRange{Offset: 4, Size: size - 4}, nil
}
func (replacementTagFormat) Rewrite(_ context.Context, src io.ReaderAt, size int64, dst io.Writer, _ tagformat.Patch) (tagformat.EditResult, error) {
	data := make([]byte, size)
	if _, err := src.ReadAt(data, 0); err != nil {
		return tagformat.EditResult{}, err
	}
	data[0] = 'N'
	_, err := dst.Write(data)
	return tagformat.EditResult{Changed: true, AudioSHA512: fmt.Sprintf("%x", sha512.Sum512(data[4:]))}, err
}

func TestReplacementTagPluginControlsFormatAndPreservesAudio(t *testing.T) {
	root := t.TempDir()
	name := "track.fixture"
	original := append(bytes.Repeat([]byte{'O'}, 4), bytes.Repeat([]byte{17}, 16)...)
	if err := os.WriteFile(filepath.Join(root, name), original, 0600); err != nil {
		t.Fatal(err)
	}
	policy, request, host := requestFor(t, root, name)
	journal, err := writeback.OpenJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	plugin := replacementTagFormat{}
	capability, err := writeback.InspectCapability(policy.Roots[0], plugin)
	if err != nil || len(capability.Formats) != 1 || capability.Formats[0] != "fixture" {
		t.Fatal("replacement format was not advertised", err)
	}
	service, err := writeback.New(policy, journal, host, plugin)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), "admin", request)
	if err != nil || !result.Changed || host.commits != 1 {
		t.Fatal("replacement format did not execute", err)
	}
	updated, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || updated[0] != 'N' || !bytes.Equal(updated[4:], original[4:]) {
		t.Fatal("replacement format changed audio", err)
	}
}

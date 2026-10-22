package sourceaccess_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func packageAccess(t *testing.T, root string) *sourceaccess.Access {
	t.Helper()
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func packageWrite(t *testing.T, root, relative string, content []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content, 0600); err != nil {
		t.Fatal(err)
	}
}

func packageWave() []byte {
	b := make([]byte, 48)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 96000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 4)
	return b
}

func packageErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestPackageSnapshotPreservesTreeAndDetectsContent(t *testing.T) {
	root := t.TempDir()
	packageWrite(t, root, "RJ123/特典/音声.bin", packageWave())
	packageWrite(t, root, "RJ123/说明.wav", []byte("This is text, not audio."))
	packageWrite(t, root, "RJ123/封面.png", []byte("\x89PNG\r\n\x1a\n"))
	packageWrite(t, root, "RJ123/封面.webp", []byte("RIFF\x0c\x00\x00\x00WEBPVP8 \x00\x00\x00\x00"))
	packageWrite(t, root, "RJ123/.hidden/secret.txt", []byte("hidden"))
	packageWrite(t, root, "RJ123/skip/.nomedia", nil)
	packageWrite(t, root, "RJ123/skip/private.wav", packageWave())
	packageWrite(t, root, "RJ123/excluded/private.wav", packageWave())
	if err := os.Mkdir(filepath.Join(root, "RJ123", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	a := packageAccess(t, root)
	options := pluginproto.ImportOptions{Recursive: false, SkipHidden: true, RespectNoMedia: true, ExcludePatterns: []string{"RJ123/excluded"}}
	tree, err := a.PackageSnapshot(context.Background(), "music", "RJ123", options)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]sourceaccess.PackageEntry{}
	for _, entry := range tree.Entries {
		entries[entry.Path] = entry
	}
	if len(entries) != 7 || entries["."].Kind != "directory" || entries["empty"].Kind != "directory" || entries["特典/音声.bin"].Kind != "audio" || entries["说明.wav"].AttachmentKind != "text" || entries["封面.png"].AttachmentKind != "image" || entries["封面.webp"].AttachmentKind != "download" {
		t.Fatalf("unexpected package tree: %+v", entries)
	}
	if entries["特典/音声.bin"].Entry.RelativePath != "RJ123/特典/音声.bin" || tree.RootIdentity == "" || tree.Signature == "" {
		t.Fatal("package lost source identity")
	}
	again, err := a.PackageSnapshot(context.Background(), "music", "RJ123", options)
	if err != nil || again.Signature != tree.Signature {
		t.Fatalf("stable tree changed: %v", err)
	}
	if err := os.Rename(filepath.Join(root, "RJ123", "empty"), filepath.Join(root, "RJ123", "renamed")); err != nil {
		t.Fatal(err)
	}
	again, err = a.PackageSnapshot(context.Background(), "music", "RJ123", options)
	if err != nil || again.Signature == tree.Signature {
		t.Fatalf("empty directory rename was not detected: %v", err)
	}
	listed, err := a.PackageDirectories(context.Background(), "music", "RJ123", options)
	if err != nil || len(listed.Directories) != 2 || listed.Path != "RJ123" {
		t.Fatalf("unexpected directory listing: %+v %v", listed, err)
	}
	_, err = a.PackageSnapshot(context.Background(), "music", "RJ123/skip", options)
	packageErrorCode(t, err, "ACCESS_DENIED")
}

func TestPackagePathsRejectEscapesAndAmbiguousNames(t *testing.T) {
	a := packageAccess(t, t.TempDir())
	for _, value := range []string{"", "..", "../outside", "/absolute", "C:/drive", `\\host\share`, `one\two`, "./a", "a//b", "a/../b", "a.", "a ", "a:stream", "a\x00b"} {
		t.Run(fmt.Sprintf("path_%x", value), func(t *testing.T) {
			if sourceaccess.ValidPackagePath(value) {
				t.Fatal("accepted unsafe package path")
			}
			_, err := a.PackageSnapshot(context.Background(), "music", value, pluginproto.ImportOptions{})
			packageErrorCode(t, err, "INVALID_ARGUMENT")
		})
	}
	if _, err := a.PackageSnapshot(context.Background(), "unknown", ".", pluginproto.ImportOptions{}); err == nil {
		t.Fatal("unknown root accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.PackageSnapshot(ctx, "music", ".", pluginproto.ImportOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled enumeration succeeded: %v", err)
	}
}

func TestPackageBudgetsFailInsteadOfTruncating(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(strings.Repeat("d/", sourceaccess.MaxPackageDepth+1))), 0700); err != nil {
			t.Fatal(err)
		}
		_, err := packageAccess(t, root).PackageSnapshot(context.Background(), "music", ".", pluginproto.ImportOptions{})
		packageErrorCode(t, err, "RATE_LIMITED")
	})
	t.Run("directories", func(t *testing.T) {
		root := t.TempDir()
		for i := 0; i <= sourceaccess.MaxPackageDirectories; i++ {
			if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("d%04d", i)), 0700); err != nil {
				t.Fatal(err)
			}
		}
		_, err := packageAccess(t, root).PackageDirectories(context.Background(), "music", ".", pluginproto.ImportOptions{})
		packageErrorCode(t, err, "RATE_LIMITED")
	})
}

func TestPackageOpenBindsVersionAndStableRead(t *testing.T) {
	root := t.TempDir()
	packageWrite(t, root, "包/说明.txt", []byte("original"))
	a := packageAccess(t, root)
	entry, err := a.Inspect("music", "包/说明.txt")
	if err != nil {
		t.Fatal(err)
	}
	scope := sourceaccess.Scope{UserID: "owner", InstanceID: "provider", TaskID: "package", AssetID: entry.ObservedVersion}
	h, err := a.OpenPackageFile(context.Background(), scope, "music", "包/说明.txt", entry.ObservedVersion, pluginproto.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stable, err := h.Stable(context.Background())
	if err != nil {
		h.Close()
		t.Fatal(err)
	}
	body, err := io.ReadAll(io.NewSectionReader(stable, 0, entry.SizeBytes))
	if err != nil || string(body) != "original" {
		t.Fatalf("unexpected stable read: %q %v", body, err)
	}
	if err = stable.Validate(); err != nil {
		t.Fatal(err)
	}
	stable.Close()
	h.Close()
	a.Revoke(scope.TaskID)
	packageWrite(t, root, "包/说明.txt", []byte("changed content"))
	if h, err := a.OpenPackageFile(context.Background(), scope, "music", "包/说明.txt", entry.ObservedVersion, pluginproto.ImportOptions{}); err == nil {
		h.Close()
		t.Fatal("accepted changed source version")
	}
}

func TestPackageRejectsLinkedAncestorsAndOfflineRoots(t *testing.T) {
	root := t.TempDir()
	packageWrite(t, root, "real/track.wav", packageWave())
	a := packageAccess(t, root)
	t.Run("links", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "linked")); err != nil {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		for _, value := range []string{".", "linked"} {
			_, err := a.PackageSnapshot(context.Background(), "music", value, pluginproto.ImportOptions{})
			packageErrorCode(t, err, "ACCESS_DENIED")
		}
		entry, err := a.Inspect("music", "real/track.wav")
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.OpenPackageFile(context.Background(), sourceaccess.Scope{TaskID: "link"}, "music", "linked/track.wav", entry.ObservedVersion, pluginproto.ImportOptions{})
		packageErrorCode(t, err, "ACCESS_DENIED")
	})
	t.Run("offline", func(t *testing.T) {
		if err := os.Rename(filepath.Join(root, "real"), filepath.Join(root, "moved")); err != nil {
			t.Fatal(err)
		}
		_, err := a.PackageSnapshot(context.Background(), "music", "real", pluginproto.ImportOptions{})
		packageErrorCode(t, err, "SOURCE_OFFLINE")
	})
}

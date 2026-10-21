package recovery_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func contentValidator(expected []byte) mediastore.Validator {
	return func(in io.ReadSeeker) error {
		got, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, expected) {
			return errors.New("invalid content")
		}
		return nil
	}
}

func TestMediaPublicationSurvivesReopenAndRefusesChangedFiles(t *testing.T) {
	root := t.TempDir()
	files, err := mediastore.OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("immutable content written before database reference")
	obj, err := files.Write(context.Background(), domain.NewID(), "fingerprint", "application/json", bytes.NewReader(body), 1024, contentValidator(body))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = files.Open(obj); err == nil {
		t.Fatal("staged content is visible as published")
	}
	files.Close()
	files, err = mediastore.OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err = files.Publish(obj); err != nil {
		t.Fatal(err)
	}
	if err = files.Publish(obj); err != nil {
		t.Fatalf("publication replay failed: %v", err)
	}
	in, err := files.Open(obj)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(in)
	in.Close()
	if err != nil || !bytes.Equal(body, got) {
		t.Fatal("published bytes changed")
	}
	if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(obj.RelativePath)), bytes.Repeat([]byte{'x'}, len(body)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = files.Open(obj); err == nil {
		t.Fatal("same-size corrupt object was served")
	}
	if err = files.Publish(obj); err == nil {
		t.Fatal("existing corrupt destination was treated as successful")
	}
}

func TestMediaRejectsInvalidInputsAndConfinesGeneratedPaths(t *testing.T) {
	root := t.TempDir()
	files, err := mediastore.OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	for _, tc := range []struct {
		name, body string
		limit      int64
		valid      bool
	}{
		{"oversize", "long", 3, true}, {"empty", "", 1, true}, {"invalid", "body", 8, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := domain.NewID()
			validator := contentValidator([]byte(tc.body))
			if !tc.valid {
				validator = contentValidator(nil)
			}
			if _, err := files.Write(context.Background(), id, "fingerprint", "application/json", bytes.NewBufferString(tc.body), tc.limit, validator); err == nil {
				t.Fatal("invalid media accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "staging", id)); !os.IsNotExist(err) {
				t.Fatal("failed write retained its partial staging file")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = files.Write(ctx, domain.NewID(), "fingerprint", "application/json", bytes.NewBufferString("body"), 8, contentValidator([]byte("body"))); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, id := range []string{"../escape", "C:/escape", "objects/file", "", "UPPER"} {
		if _, err = files.Write(context.Background(), id, "fingerprint", "application/json", bytes.NewBufferString("body"), 8, contentValidator([]byte("body"))); err == nil {
			t.Fatal("untrusted file name accepted")
		}
	}
	obj, err := files.Write(context.Background(), domain.NewID(), "fingerprint", "application/json", bytes.NewBufferString("body"), 8, contentValidator([]byte("body")))
	if err != nil {
		t.Fatal(err)
	}
	obj.RelativePath = "../escape"
	if err = files.Publish(obj); err == nil {
		t.Fatal("database path escaped media root")
	}
}

func TestMediaRejectsRedirectedStorageDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "objects")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if files, err := mediastore.OpenFiles(root); err == nil {
		files.Close()
		t.Fatal("redirected media directory accepted")
	}
}

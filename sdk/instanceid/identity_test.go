package instanceid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyIdentityIsStableUUID(t *testing.T) {
	id := Canonical("local")
	if id != "b1548e96-3081-5c9c-8376-bf290e369d51" || !Valid(id) || Canonical(id) != id || Canonical("LOCAL") != id {
		t.Fatal("legacy identity is not a stable canonical UUID")
	}
	if Canonical("other") == id || Valid(Canonical("00000000-0000-0000-0000-000000000000")) || Valid(Canonical("../local")) {
		t.Fatal("invalid identity accepted or separate instances merged")
	}
}

func TestLegacyStateDirectoryAndCollision(t *testing.T) {
	base := t.TempDir()
	id := Canonical("local")
	legacy := filepath.Join(base, "local")
	if err := os.Mkdir(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := StateDirectory(base, id)
	if err != nil || got != legacy {
		t.Fatalf("legacy state was not retained: %v", err)
	}
	if err := os.Mkdir(filepath.Join(base, id), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := StateDirectory(base, id); err == nil {
		t.Fatal("ambiguous state directories were accepted")
	}
	if _, err := StateDirectory(base, "../escape"); err == nil {
		t.Fatal("invalid state identity accepted")
	}
}

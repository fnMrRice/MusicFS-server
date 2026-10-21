package accountstore_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/accountstore"
)

func TestEncryptedPersistentScopedCredentialAndDeletion(t *testing.T) {
	dir := t.TempDir()
	s, err := accountstore.Open(dir, "owner/instance")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(`{"token":"synthetic-account-secret"}`)
	if err = s.Save(secret); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "account.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, secret) || bytes.Contains(raw, []byte("synthetic-account-secret")) {
		t.Fatal("plaintext credential on disk")
	}
	restored, err := accountstore.Open(dir, "owner/instance")
	if err != nil {
		t.Fatal(err)
	}
	value, err := restored.Load()
	if err != nil || !bytes.Equal(value, secret) {
		t.Fatal("credential did not survive restart")
	}
	other, err := accountstore.Open(dir, "other/instance")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Load(); err == nil {
		t.Fatal("cross-owner credential accepted")
	}
	raw[len(raw)-1] ^= 1
	if err = os.WriteFile(filepath.Join(dir, "account.bin"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(); err == nil {
		t.Fatal("tampering accepted")
	}
	if err = s.Save(nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(); err != nil {
		t.Fatal(err)
	}
}

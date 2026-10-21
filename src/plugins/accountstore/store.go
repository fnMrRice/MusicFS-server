// Package accountstore persists Host-owned platform credentials outside config.
package accountstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Store struct {
	dir   string
	aead  cipher.AEAD
	scope []byte
}

func Open(dir, scope string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, domain.Fail("ACCESS_DENIED", "Platform credential directory is invalid")
	}
	if err = protect(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "key")
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return nil, domain.Fail("ACCESS_DENIED", "Platform credential key is invalid")
	}
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		var f *os.File
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		if err = protect(path); err == nil {
			_, err = f.Write(key)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Platform credential key is invalid")
	}
	if err = protect(path); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir, aead: aead, scope: []byte(scope)}, nil
}
func (s *Store) Load() ([]byte, error) {
	path := filepath.Join(s.dir, "account.bin")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<10 {
		return nil, domain.Fail("ACCESS_DENIED", "Stored platform account is invalid")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Stored platform account is damaged")
	}
	return s.aead.Open(nil, raw[:n], raw[n:], s.scope)
}
func (s *Store) Save(secret []byte) error {
	if len(secret) == 0 {
		err := os.Remove(filepath.Join(s.dir, "account.bin"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(secret) > 64<<10 {
		return domain.Fail("INVALID_ARGUMENT", "Platform credential payload is too large")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	raw := s.aead.Seal(nonce, nonce, secret, s.scope)
	f, err := os.CreateTemp(s.dir, "account-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = protect(f.Name()); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return replace(f.Name(), filepath.Join(s.dir, "account.bin"))
}

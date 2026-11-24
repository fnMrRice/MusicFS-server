package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/options"
)

const postgresYAML = minimalYAML + "plugins: []\nstorage:\n  backend: postgres\n  options:\n"
const requiredPostgresFields = "    host: db.invalid\n    database: musicfs\n    user: musicfs\n"

func TestPostgresStorageMinimalYAMLAndCredentialPaths(t *testing.T) {
	doc, err := parse(t, postgresYAML+requiredPostgresFields)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := options.Parse(doc.Config.Storage.Options)
	if err != nil || settings.Port != "5432" || settings.SSLMode != "verify-full" {
		t.Fatalf("unexpected PostgreSQL defaults: %q %q, %v", settings.Port, settings.SSLMode, err)
	}
	doc, err = parse(t, postgresYAML+requiredPostgresFields+"    passwordFile: secrets/password\n    sslRootCert: tls/root.pem\n")
	if err != nil {
		t.Fatal(err)
	}
	for field, path := range map[string]string{"passwordFile": "secrets/password", "sslRootCert": "tls/root.pem"} {
		want := filepath.Join(filepath.Dir(doc.Path), filepath.FromSlash(path))
		if got := doc.Config.Storage.Options[field]; got != want {
			t.Errorf("%s path resolved to %q, expected %q", field, got, want)
		}
	}
	if _, err := parse(t, postgresYAML+requiredPostgresFields+"    passwordEnv: MUSICFS_TEST_UNSET_CREDENTIAL\n"); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresStorageRejectsInvalidOptionsBeforeOpeningDatabase(t *testing.T) {
	const secret = "private-option-sentinel"
	cases := []struct {
		name, fields, message string
	}{
		{"plaintext password", requiredPostgresFields + "    password: " + secret + "\n", "unknown PostgreSQL storage option"},
		{"unknown key", requiredPostgresFields + "    " + secret + ": unused\n", "unknown PostgreSQL storage option"},
		{"two password sources", requiredPostgresFields + "    passwordFile: " + secret + "\n    passwordEnv: PRIVATE_SECRET\n", "select one PostgreSQL password source"},
		{"missing host", "    database: musicfs\n    user: musicfs\n", "requires host, database and user"},
		{"blank database", strings.Replace(requiredPostgresFields, "database: musicfs", "database: '   '", 1), "requires host, database and user"},
		{"empty user", strings.Replace(requiredPostgresFields, "user: musicfs", "user: ''", 1), "requires host, database and user"},
		{"empty credential file", requiredPostgresFields + "    passwordFile: ''\n", "empty PostgreSQL storage option"},
		{"empty port", requiredPostgresFields + "    port: ''\n", "invalid PostgreSQL port"},
		{"zero port", requiredPostgresFields + "    port: '0'\n", "invalid PostgreSQL port"},
		{"large port", requiredPostgresFields + "    port: '65536'\n", "invalid PostgreSQL port"},
		{"invalid port", requiredPostgresFields + "    port: " + secret + "\n", "invalid PostgreSQL port"},
		{"empty TLS mode", requiredPostgresFields + "    sslMode: ''\n", "invalid PostgreSQL sslMode"},
		{"invalid TLS mode", requiredPostgresFields + "    sslMode: " + secret + "\n", "invalid PostgreSQL sslMode"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, postgresYAML+tt.fields)
			if err == nil || !strings.Contains(err.Error(), tt.message) || strings.Contains(err.Error(), secret) {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

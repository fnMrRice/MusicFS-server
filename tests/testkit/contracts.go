package testkit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// RepositoryRoot locates checked-in fixtures independently of the test cwd.
func RepositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

// ReadJSON reads JSON while preserving integer precision for schema checks.
func ReadJSON(t testing.TB, path string) any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(RepositoryRoot(), filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return DecodeJSON(t, b)
}

func DecodeJSON(t testing.TB, b []byte) any {
	t.Helper()
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// ContractSchema compiles only local, version-controlled schema resources.
func ContractSchema(t testing.TB, family, name string) *jsonschema.Schema {
	t.Helper()
	path := "docs/contracts/http.schema.json"
	url := "https://musicfs.invalid/contracts/http.schema.json"
	if family == "plugin" {
		path = "docs/contracts/plugins/v1/protocol.schema.json"
		url = "https://musicfs.invalid/contracts/plugins/v1/protocol.schema.json"
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(url, ReadJSON(t, path)); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		url += "#/$defs/" + name
	}
	schema, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

// ValidateHTTP checks a real serialized response or request against its model.
func ValidateHTTP(t testing.TB, name string, body []byte) {
	t.Helper()
	if err := ContractSchema(t, "http", name).Validate(DecodeJSON(t, body)); err != nil {
		t.Fatalf("HTTP %s contract mismatch: %v", name, err)
	}
}

// ValidatePlugin checks the actual Go wire DTO, including its JSON field tags.
func ValidatePlugin(t testing.TB, name string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := ContractSchema(t, "plugin", name).Validate(DecodeJSON(t, b)); err != nil {
		t.Fatalf("plugin %s contract mismatch: %v", name, err)
	}
}

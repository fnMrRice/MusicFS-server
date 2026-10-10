package testkit

import (
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func ValidateScrapeDocument(t testing.TB, body []byte) {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	for _, name := range []string{"http", "scrape-document"} {
		if err := c.AddResource("https://musicfs.invalid/contracts/"+name+".schema.json", ReadJSON(t, "docs/contracts/"+name+".schema.json")); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := c.Compile("https://musicfs.invalid/contracts/scrape-document.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(DecodeJSON(t, body)); err != nil {
		t.Fatalf("scrape document contract: %v", err)
	}
}

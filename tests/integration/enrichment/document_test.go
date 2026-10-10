package enrichment_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestCompleteScrapeDocumentStoresResultsWithoutCapabilities(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	j, _, err := f.service.Submit(ctx, "admin", request(f.good().ID), "document-attempt")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	j = finished(t, f, j.JobID)
	if j.State != "completed" {
		t.Fatalf("job: %+v", j)
	}
	items, err := f.content.Derived().ListScrapeDocuments(ctx, "admin", j.JobID)
	if err != nil || len(items) != 1 {
		t.Fatalf("document list: %+v %v", items, err)
	}
	_, in, err := f.content.Derived().Open(ctx, "admin", items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(in)
	in.Close()
	if err != nil {
		t.Fatal(err)
	}
	var d enrichment.ScrapeDocument
	if err = json.Unmarshal(body, &d); err != nil {
		t.Fatal(err)
	}
	testkit.ValidateScrapeDocument(t, body)
	if len(d.Targets) != 1 || d.Targets[0] != d.Target {
		t.Fatal("complete job target snapshot is missing")
	}
	if d.Schema != "scrape-document/v1" || d.JobID != j.JobID || d.Target != j.Targets[0].Target || len(d.Resources) == 0 || len(d.Attempts) != 1 || len(d.Outcome.Candidates) != 3 {
		t.Fatalf("incomplete document: %+v", d)
	}
	a := d.Attempts[0]
	if a.PluginVersion == "" || a.PluginID == "" || a.Search == nil || len(a.Search.Candidates) != 3 || len(a.Fetches) != 3 {
		t.Fatalf("incomplete attempt: %+v", a)
	}
	for _, fetch := range a.Fetches {
		if fetch.State != "completed" || fetch.Preview == nil || fetch.Preview.PreviewObjectID == nil {
			t.Fatalf("fetch: %+v", fetch)
		}
	}
	for _, private := range []string{"resourceRef", "http://127.0.0.1", f.root, "Embedded fixture lyrics"} {
		if bytes.Contains(body, []byte(private)) {
			t.Fatalf("private capability, path or content body in document: %s", private)
		}
	}
	var raw []byte
	if err = f.store.DB.QueryRow("SELECT document FROM media_derived_objects WHERE id=?", items[0].ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("candidates")) || bytes.Contains(raw, []byte("fetches")) {
		t.Fatal("document body duplicated in database")
	}
	if _, err = f.content.Derived().ListScrapeDocuments(ctx, "bob", j.JobID); err == nil {
		t.Fatal("foreign owner listed documents")
	}
	if _, err = f.store.DB.Exec("UPDATE tracks SET owner='bob' WHERE id=?", f.good().ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.content.Derived().Open(ctx, "admin", items[0].ID); err == nil {
		t.Fatal("document bypassed original source ownership")
	}
	items, err = f.content.Derived().ListScrapeDocuments(ctx, "admin", j.JobID)
	if err != nil || len(items) != 0 {
		t.Fatal("document list bypassed original source ownership", err)
	}
}

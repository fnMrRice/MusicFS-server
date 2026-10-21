package e2e_test

import (
	"bytes"
	"encoding/json"
	"image/png"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func albumRequest[T any](t *testing.T, c *client, method, path string, input any, key string, status int, schema string) T {
	t.Helper()
	code, raw, _ := c.request(t, method, "/api/v1"+path, input, key)
	var out T
	if code != status || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("%s %s: %d %s", method, path, code, raw)
	}
	testkit.ValidateHTTP(t, schema, raw)
	return out
}

func checkAlbumDocuments(t *testing.T, c *client, job enrichment.Job, music string) []mediastore.DerivedObject {
	t.Helper()
	page := albumRequest[struct {
		Items []mediastore.DerivedObject `json:"items"`
	}](t, c, "GET", "/metadata-jobs/"+job.JobID+"/documents", nil, "", 200, "ScrapeDocuments")
	if len(page.Items) != len(job.Targets) {
		t.Fatalf("missing complete scrape documents: %+v", page.Items)
	}
	for _, object := range page.Items {
		info := albumRequest[mediastore.DerivedObject](t, c, "GET", "/media/"+object.ID+"/info", nil, "", 200, "MediaObjectInfo")
		if info.ID != object.ID || object.Kind != "scrape-document" {
			t.Fatal("document index mismatch")
		}
		code, raw, headers := c.request(t, "GET", "/api/v1/media/"+object.ID, nil, "")
		var document enrichment.ScrapeDocument
		if code != 200 || json.Unmarshal(raw, &document) != nil || headers.Get("Content-Type") != "application/json" || !strings.HasPrefix(headers.Get("Content-Disposition"), "attachment;") {
			t.Fatalf("scrape document content: %d %s", code, raw)
		}
		if document.Schema != "scrape-document/v1" || document.JobID != job.JobID || document.Request.SourceAlbum == nil || document.Request.SourceAlbum.ExpectedRevision != 1 || len(document.Targets) != 2 || len(document.Attempts) != 1 || len(document.Resources) < 2 {
			t.Fatalf("incomplete scrape document: %+v", document)
		}
		attempt := document.Attempts[0]
		if attempt.PluginID != "musicfs.scraper.local" || attempt.PluginVersion == "" || attempt.Search == nil || len(attempt.Search.Candidates) < 2 || len(attempt.Fetches) < 2 || len(document.Outcome.Candidates) < 2 {
			t.Fatalf("missing structured search/fetch: %+v", document)
		}
		for _, hidden := range []string{`"resourceRef"`, `"grantId"`, `"relativePath"`, `"contentRef"`, music, strings.ReplaceAll(music, `\`, `\\`)} {
			if bytes.Contains(raw, []byte(hidden)) {
				t.Fatalf("scrape document leaked a capability or path: %s", hidden)
			}
		}
	}
	return page.Items
}

func checkAlbumThumbnail(t *testing.T, c *client, thumbnail mediastore.DerivedObject, original []byte) {
	t.Helper()
	code, raw, headers := c.request(t, "GET", "/api/v1/media/"+thumbnail.ID, nil, "")
	if code != 200 || headers.Get("Content-Type") != "image/png" {
		t.Fatal("thumbnail body unavailable")
	}
	actual, err := png.DecodeConfig(bytes.NewReader(raw))
	source, sourceErr := png.DecodeConfig(bytes.NewReader(original))
	if err != nil || sourceErr != nil || actual.Width > 256 || actual.Height > 256 || actual.Width > source.Width || actual.Height > source.Height {
		t.Fatal("thumbnail is invalid or upscaled")
	}
	albumRequest[mediastore.DerivedObject](t, c, "GET", "/media/"+thumbnail.ID+"/info", nil, "", 200, "MediaObjectInfo")
}

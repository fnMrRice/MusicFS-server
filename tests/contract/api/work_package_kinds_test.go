package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestWorkPackageKindIsRequiredAndCDCopiesKeepContentType(t *testing.T) {
	for _, contentType := range []string{"asmr", "cd_album"} {
		t.Run(contentType, func(t *testing.T) {
			f := newPackageHTTP(t)
			if contentType == "cd_album" {
				work, err := f.store.CreateWork(t.Context(), "admin", works.CreateRequest{ContentType: contentType, Title: "手动抓轨专辑"}, "cd-work-create")
				if err != nil {
					t.Fatal(err)
				}
				f.workID = work.ID
			}
			for _, kind := range []any{nil, "", "physical_disc"} {
				body := map[string]any{"providerInstanceId": packageProvider, "rootId": "music", "path": "RJ123456"}
				if kind != nil {
					body["kind"] = kind
				}
				response := f.request(t, "POST", "/package-previews", body, "", nil)
				if response.Code != 400 {
					t.Fatal("invalid or missing copy kind accepted", response.Code, response.Body.String())
				}
			}
			preview := f.previewKind(t, "cd_dump")
			if preview.Package.Kind != "cd_dump" {
				t.Fatal("preview lost the selected copy kind")
			}
			response := f.request(t, "POST", "/packages", map[string]string{"previewId": preview.ID}, "cd-import-key", nil)
			var imported workpackages.Package
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &imported) != nil || imported.Kind != "cd_dump" {
				t.Fatal("CD import failed", response.Code, response.Body.String())
			}
			testkit.ValidateHTTP(t, "WorkPackage", response.Body.Bytes())
			assertPackageKindQuery(t, f, "packageKind=cd_dump", 1)
			assertPackageKindQuery(t, f, "packageKind=cd_dump&contentType="+contentType, 1)
			assertPackageKindQuery(t, f, "packageKind=directory", 0)
			work, err := f.store.Work(t.Context(), "admin", f.workID)
			if err != nil || work.ContentType != contentType {
				t.Fatal("CD copy changed the work content type", err)
			}
			preview = f.previewKind(t, "directory")
			response = f.request(t, "POST", "/packages", map[string]string{"previewId": preview.ID}, "reclassify-copy-key", nil)
			var updated workpackages.Package
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &updated) != nil || updated.ID != imported.ID || updated.Kind != "directory" || updated.Revision != 2 {
				t.Fatal("reimport did not update the original copy", response.Code, response.Body.String())
			}
			assertPackageKindQuery(t, f, "packageKind=cd_dump", 0)
			assertPackageKindQuery(t, f, "packageKind=directory", 1)
		})
	}
}

func assertPackageKindQuery(t *testing.T, f *packageHTTPFixture, query string, count int) {
	t.Helper()
	request := httptest.NewRequest("GET", "/api/v1/works?"+query, nil)
	request.AddCookie(f.cookie)
	response := httptest.NewRecorder()
	f.server.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("package kind query failed", response.Code, response.Body.String())
	}
	testkit.ValidateHTTP(t, "Works", response.Body.Bytes())
	var result struct {
		Items []works.Work `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result.Items) != count {
		t.Fatal("package kind query returned unexpected works", query, response.Body.String(), err)
	}
	if count == 1 && result.Items[0].ID != f.workID {
		t.Fatal("package kind query returned another work")
	}
}

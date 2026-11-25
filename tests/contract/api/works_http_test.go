package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/plugins/builtinplugins"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestWorksHTTPRegistrationEditingPrivacyAndQueries(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	registry, err := contenttypes.New(builtinplugins.ContentTypes()...)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterWorks(works.New(store, registry))
	request := func(method, path string, body any, key string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	w := request("GET", "/content-types", nil, "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "voice_actor") {
		t.Fatalf("content types: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "ContentTypes", w.Body.Bytes())
	create := works.CreateRequest{ContentType: "asmr", Title: " 雨夜作品 ", Description: "中文资料",
		ExternalIDs: []works.ExternalID{{Namespace: "dlsite", Value: "RJ123456"}},
		Credits:     []works.Credit{{Role: "voice_actor", Name: "声优甲"}}, Tags: []string{"雨声"}}
	w = request("POST", "/works", create, "create-work-one", nil)
	var one works.Work
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &one) != nil || one.Title != "雨夜作品" || one.Revision != 1 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "Work", w.Body.Bytes())
	replay := request("POST", "/works", create, "create-work-one", nil)
	if replay.Code != 201 || replay.Body.String() != w.Body.String() {
		t.Fatal("create did not replay original result")
	}
	create.Title = "另一个作品"
	if w = request("POST", "/works", create, "create-work-one", nil); w.Code != 409 {
		t.Fatalf("idempotency conflict: %d", w.Code)
	}
	w = request("POST", "/works", create, "create-work-two", nil)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	for _, query := range []string{"?contentType=asmr", "?q=RJ123456", "?tag=雨声", "?credit=声优甲"} {
		w = request("GET", "/works"+query, nil, "", nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), one.ID) {
			t.Fatalf("query %s: %d %s", query, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Works", w.Body.Bytes())
	}
	w = request("GET", "/works?limit=1", nil, "", nil)
	var page struct {
		Items []works.Work `json:"items"`
		Next  *string      `json:"nextAfter"`
	}
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Next == nil {
		t.Fatal("missing next page", w.Body.String())
	}
	first := page.Items[0].ID
	w = request("GET", "/works?limit=1&after="+*page.Next, nil, "", nil)
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].ID == first || page.Next != nil {
		t.Fatal("invalid final page", w.Body.String())
	}
	update := works.UpdateRequest{ExpectedRevision: 1, Title: "修改后的作品", Description: "",
		ExternalIDs: []works.ExternalID{}, Credits: []works.Credit{}, Tags: []string{}}
	w = request("PUT", "/works/"+one.ID, update, "update-work-one", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "Work", w.Body.Bytes())
	if retry := request("PUT", "/works/"+one.ID, update, "update-work-one", nil); retry.Code != 200 || retry.Body.String() != w.Body.String() {
		t.Fatal("update replay failed")
	}
	if w = request("PUT", "/works/"+one.ID, update, "update-work-stale", nil); w.Code != 409 {
		t.Fatalf("stale revision accepted: %d", w.Code)
	}
	private, err := store.CreateWork(context.Background(), "other", create, "private-work-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if w = request(method, "/works/"+private.ID, update, "private-work-change", nil); w.Code != 404 {
			t.Fatalf("private work exposed: %d", w.Code)
		}
	}
	for _, path := range []string{"/works?limit=101", "/works?limit=0", "/works?q=a&q=b", "/works?unknown=x", "/works?after=", "/works?packageKind=physical_disc", "/works?packageKind=", "/works?packageKind=directory&packageKind=cd_dump", "/content-types?q=x"} {
		if w = request("GET", path, nil, "", nil); w.Code != 400 {
			t.Fatalf("invalid query accepted: %s %d", path, w.Code)
		}
	}
	if w = request("GET", "/content-types", nil, "", func(r *http.Request) { r.Header.Del("Cookie") }); w.Code != 401 {
		t.Fatal("anonymous content types allowed")
	}
	if w = request("POST", "/works", create, "missing-csrf-work", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	create.Credits = []works.Credit{{Role: "artist", Name: "错误角色"}}
	if w = request("POST", "/works", create, "invalid-work-role", nil); w.Code != 400 {
		t.Fatal("undeclared role accepted")
	}
	create.Credits = []works.Credit{}
	for _, id := range []string{"unknown_type", "music"} {
		create.ContentType = id
		if w = request("POST", "/works", create, "unsupported-"+id, nil); w.Code != 415 {
			t.Fatalf("unregistered content type %s returned %d", id, w.Code)
		}
	}
	for _, body := range []any{map[string]any{"contentType": "asmr", "title": "missing fields"},
		map[string]any{"contentType": "asmr", "title": "x", "description": "", "credits": nil, "externalIds": []any{}, "tags": []string{}}} {
		if w = request("POST", "/works", body, "missing-fields-key", nil); w.Code != 400 {
			t.Fatal("missing explicit fields accepted")
		}
	}
}

func TestWorkReadSurvivesUnregisteredType(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	registry, _ := contenttypes.New()
	server.RegisterWorks(works.New(store, registry))
	one, err := store.CreateWork(context.Background(), "admin", works.CreateRequest{ContentType: "removed_type", Title: "保留资料"}, "retained-work-key")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/works/"+one.ID, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("retained work unreadable: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "Work", w.Body.Bytes())
}

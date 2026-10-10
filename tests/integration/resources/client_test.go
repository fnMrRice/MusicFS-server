package resources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
)

type client struct {
	http       *http.Client
	base, csrf string
}

func login(t *testing.T, base, token string) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{http: &http.Client{Jar: jar, Timeout: 20 * time.Second}, base: base}
	r, _ := http.NewRequest("POST", base+"/api/v1/auth/session", nil)
	r.Header.Set("Origin", base)
	r.Header.Set("Authorization", "Bearer "+token)
	response, err := c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		CSRF string `json:"csrfToken"`
	}
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&body) != nil || body.CSRF == "" {
		t.Fatal("login failed", response.StatusCode)
	}
	c.csrf = body.CSRF
	return c
}

func (c *client) request(method, path string, value any, key string, target any) error {
	var body io.Reader
	if value != nil {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, c.base+"/api/v1"+path, body)
	if err != nil {
		return err
	}
	r.Header.Set("Origin", c.base)
	r.Header.Set("X-CSRF-Token", c.csrf)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	response, err := c.http.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s %s: %d %s", method, path, response.StatusCode, b)
	}
	if target != nil {
		return json.NewDecoder(response.Body).Decode(target)
	}
	_, err = io.Copy(io.Discard, response.Body)
	return err
}

func scan(t *testing.T, c *client) []library.Track {
	t.Helper()
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	check(t, c.request("GET", "/provider-instances", nil, "", &providers))
	if len(providers.Items) != 1 || providers.Items[0].Configuration == nil {
		t.Fatal("missing provider")
	}
	p := providers.Items[0]
	var accepted struct {
		ID string `json:"jobId"`
	}
	check(t, c.request("POST", "/provider-instances/local/scan-jobs", map[string]any{
		"mode": "full", "schemaRevision": p.Configuration.SchemaRevision, "rootIds": []string{"local"}, "options": map[string]any{},
	}, "resource-scan", &accepted))
	for deadline := time.Now().Add(60 * time.Second); ; {
		var job struct {
			Status string `json:"status"`
		}
		check(t, c.request("GET", "/scan-jobs/"+accepted.ID, nil, "", &job))
		if job.Status == "succeeded" {
			break
		}
		if job.Status == "failed" || time.Now().After(deadline) {
			t.Fatal("scan failed", job.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var page library.Page
	check(t, c.request("GET", "/source-tracks?limit=50", nil, "", &page))
	return page.Items
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

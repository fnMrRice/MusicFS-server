package testkit

import (
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// DecodeNeteaseFixture lets metadata fixtures inspect the encrypted client
// request as logical API parameters. It is only used by test transports.
func DecodeNeteaseFixture(r *http.Request) (*http.Request, error) {
	if !strings.HasPrefix(r.URL.Path, "/eapi/") {
		return r, nil
	}
	if r.Method != "POST" || r.URL.Host != "interface.music.163.com" {
		return nil, fmt.Errorf("expected EAPI client request")
	}
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(r.Form.Get("params"))
	if err != nil || len(b) == 0 || len(b)%16 != 0 {
		return nil, fmt.Errorf("invalid EAPI payload")
	}
	cipher, _ := aes.NewCipher([]byte("e82ckenh8dichen8"))
	for i := 0; i < len(b); i += 16 {
		cipher.Decrypt(b[i:i+16], b[i:i+16])
	}
	pad := int(b[len(b)-1])
	if pad < 1 || pad > 16 {
		return nil, fmt.Errorf("invalid padding")
	}
	parts := strings.Split(string(b[:len(b)-pad]), "-36cd479b6b5-")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid EAPI framing")
	}
	var data map[string]any
	if err = json.Unmarshal([]byte(parts[1]), &data); err != nil {
		return nil, err
	}
	q := url.Values{}
	for k, v := range data {
		if k != "header" && k != "e_r" {
			q.Set(k, fmt.Sprint(v))
		}
	}
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(strings.NewReader(r.Form.Encode()))
	clone.ContentLength = int64(len(r.Form.Encode()))
	clone.URL = &url.URL{Scheme: "https", Host: "music.163.com", Path: parts[0], RawQuery: q.Encode()}
	return clone, nil
}

type accountFixtureTransport struct{ next http.RoundTripper }

func (f accountFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/eapi/nuser/account/get" {
		return fixtureJSON(`{"code":200,"profile":{"userId":12345,"nickname":"Fixture"}}`), nil
	}
	if r.URL.Host == "u.y.qq.com" {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(b)))
		if strings.Contains(string(b), "GetLoginUserInfo") {
			return fixtureJSON(`{"code":0,"req_0":{"code":0,"data":{"uin":12345}}}`), nil
		}
	}
	decoded, err := DecodeNeteaseFixture(r)
	if err != nil {
		return nil, err
	}
	return f.next.RoundTrip(decoded)
}
func fixtureJSON(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func OnlineAccountTransport(next http.RoundTripper) http.RoundTripper {
	return accountFixtureTransport{next}
}

func RestoreOnlineFixture(t *testing.T, c interface {
	Account(context.Context, online.AccountRequest) (online.AccountResult, error)
}, platform, owner, instance string) {
	t.Helper()
	secret, _ := json.Marshal(map[string]any{"platform": platform, "userId": owner, "instanceId": instance, "deviceId": "fixture-device", "accountId": "12345", "cookies": map[string]string{"MUSIC_U": "fixture-private-token"}, "musicKey": "fixture-private-token"})
	if _, err := c.Account(context.Background(), online.AccountRequest{Action: "restore", Secret: secret}); err != nil {
		t.Fatal(err)
	}
	result, err := c.Account(context.Background(), online.AccountRequest{Action: "check"})
	if err != nil || result.View.State != "signedIn" {
		t.Fatalf("fixture account check: %v", err)
	}
	time.Sleep(1010 * time.Millisecond)
}

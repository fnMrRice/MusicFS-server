package online_test

import (
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/online"
)

func accountResponse(body string, cookies ...string) *http.Response {
	h := http.Header{}
	for _, v := range cookies {
		h.Add("Set-Cookie", v)
	}
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}
func decodeEAPI(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	if r.Method != "POST" || r.URL.Host != "interface.music.163.com" || !strings.Contains(r.UserAgent(), "NeteaseMusicDesktop") {
		t.Fatal("not a client request")
	}
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(r.Form.Get("params"))
	if err != nil || len(raw)%16 != 0 {
		t.Fatal("invalid eapi frame")
	}
	block, _ := aes.NewCipher([]byte("e82ckenh8dichen8"))
	for i := 0; i < len(raw); i += 16 {
		block.Decrypt(raw[i:i+16], raw[i:i+16])
	}
	raw = raw[:len(raw)-int(raw[len(raw)-1])]
	parts := strings.Split(string(raw), "-36cd479b6b5-")
	if len(parts) != 3 {
		t.Fatal("invalid eapi body")
	}
	if parts[0] != "/api/"+strings.TrimPrefix(r.URL.Path, "/eapi/") {
		t.Fatal("eapi path mismatch")
	}
	digest := md5.Sum([]byte("nobody" + parts[0] + "use" + parts[1] + "md5forencrypt"))
	if parts[2] != fmt.Sprintf("%x", digest) {
		t.Fatal("invalid eapi digest")
	}
	var data map[string]any
	if json.Unmarshal([]byte(parts[1]), &data) != nil {
		t.Fatal("invalid eapi payload")
	}
	return data
}
func TestNeteaseAccountLifecycleAndPrivateSnapshot(t *testing.T) {
	t.Parallel()
	poll := 0
	c := anonymousScoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		data := decodeEAPI(t, r)
		switch r.URL.Path {
		case "/eapi/login/qrcode/unikey":
			return accountResponse(`{"code":200,"unikey":"test-challenge"}`), nil
		case "/eapi/login/qrcode/client/login":
			if data["key"] != "test-challenge" {
				t.Fatal("wrong challenge")
			}
			poll++
			if poll == 1 {
				return accountResponse(`{"code":802}`), nil
			}
			return accountResponse(`{"code":803}`, "MUSIC_U=fixture-private-token; Path=/; Secure"), nil
		case "/eapi/nuser/account/get":
			cookie, err := r.Cookie("MUSIC_U")
			if err != nil || cookie.Value != "fixture-private-token" {
				t.Fatal("missing platform session")
			}
			return accountResponse(`{"code":200,"profile":{"userId":12345,"nickname":"Test listener"}}`), nil
		default:
			t.Fatal("unexpected endpoint")
			return nil, nil
		}
	}))
	ctx := context.Background()
	begin, err := c.Account(ctx, online.AccountRequest{Action: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	if begin.View.Method != "qr" || !strings.HasPrefix(begin.View.QRURL, "https://music.163.com/login?codekey=") {
		t.Fatal("no login challenge")
	}
	_, err = c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
	expectCode(t, err, "RATE_LIMITED")
	time.Sleep(3100 * time.Millisecond)
	scanned, err := c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
	if err != nil || scanned.View.State != "scanned" {
		t.Fatalf("scan state: %v", err)
	}
	time.Sleep(3100 * time.Millisecond)
	done, err := c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
	if err != nil {
		t.Fatal(err)
	}
	if done.View.State != "signedIn" || done.View.AccountID != "12345" || len(done.Secret) == 0 {
		t.Fatal("missing authenticated binding")
	}
	public, _ := json.Marshal(done.View)
	if strings.Contains(string(public), "fixture-private-token") {
		t.Fatal("secret in public state")
	}
	other, err := online.New(online.Netease, online.Scope{UserID: "other", InstanceID: "instance"}, online.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	_, err = other.Account(ctx, online.AccountRequest{Action: "restore", Secret: done.Secret})
	expectCode(t, err, "ACCESS_DENIED")
	restored := anonymousScoped(t, online.Netease, response(`{}`))
	v, err := restored.Account(ctx, online.AccountRequest{Action: "restore", Secret: done.Secret})
	if err != nil || v.View.State != "unverified" {
		t.Fatal("restored credential incorrectly trusted")
	}
	v, err = c.Account(ctx, online.AccountRequest{Action: "logout"})
	if err != nil || len(v.Secret) != 0 || v.View.State != "signedOut" {
		t.Fatal("logout retained credentials")
	}
	_, err = c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
	expectCode(t, err, "NOT_FOUND")
}

func TestCanceledLoginCannotComplete(t *testing.T) {
	t.Parallel()
	c := anonymousScoped(t, online.Netease, response(`{"code":200,"unikey":"fixture"}`))
	ctx := context.Background()
	begin, err := c.Account(ctx, online.AccountRequest{Action: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Account(ctx, online.AccountRequest{Action: "cancel", LoginID: begin.View.LoginID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
	expectCode(t, err, "NOT_FOUND")
}
func TestAppleRequiresUserTokenAndValidatesWithApple(t *testing.T) {
	t.Parallel()
	c := anonymousScoped(t, online.AppleMusic, roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.music.apple.com" || r.URL.Path != "/v1/me/storefront" || r.Header.Get("Authorization") != "Bearer developer-fixture" || r.Header.Get("Music-User-Token") != "user-fixture" {
			t.Fatal("invalid MusicKit authorization request")
		}
		return accountResponse(`{"data":[{"id":"us","type":"storefronts"}]}`), nil
	}))
	ctx := context.Background()
	begin, err := c.Account(ctx, online.AccountRequest{Action: "begin", DeveloperToken: "developer-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if begin.View.Method != "musicKit" || begin.View.State != "waiting" {
		t.Fatal("developer token treated as user login")
	}
	time.Sleep(3100 * time.Millisecond)
	result, err := c.Account(ctx, online.AccountRequest{Action: "complete", LoginID: begin.View.LoginID, UserToken: "user-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if result.View.State != "signedIn" || result.View.AccountID == "us" {
		t.Fatal("invalid Apple binding")
	}
}
func TestQQQRAndDesktopAccountExchange(t *testing.T) {
	t.Parallel()
	c := anonymousScoped(t, online.QQMusic, roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/ptqrshow":
			return accountResponse("\x89PNG\r\n\x1a\nfixture", "qrsig=fixture-signature; Secure"), nil
		case "/ptqrlogin":
			return accountResponse(`ptuiCB('0','0','https://ssl.ptlogin2.graph.qq.com/check_sig?uin=12345&ptsigx=fixture-signature&service=ptqrlogin','0','OK','Test');`), nil
		case "/check_sig":
			return accountResponse("", "p_skey=fixture-session; Secure"), nil
		case "/oauth2.0/authorize":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			res := accountResponse("")
			res.StatusCode = 302
			res.Header.Set("Location", "https://y.qq.com/portal/wx_redirect.html?code=fixture-code&state="+url.QueryEscape(r.Form.Get("state")))
			return res, nil
		case "/cgi-bin/musicu.fcg":
			var data struct {
				Comm    map[string]string `json:"comm"`
				Request struct {
					Method string `json:"method"`
				} `json:"req_0"`
			}
			if json.NewDecoder(r.Body).Decode(&data) != nil {
				t.Fatal("invalid desktop RPC")
			}
			if data.Comm["ct"] != "19" || data.Comm["cv"] != "2201" {
				t.Fatal("missing desktop profile")
			}
			if data.Request.Method == "QQLogin" {
				return accountResponse(`{"code":0,"req_0":{"code":0,"data":{"musicid":12345,"musickey":"fixture-key","nick":"Test"}}}`), nil
			}
			if data.Comm["authst"] != "fixture-key" {
				t.Fatal("missing account context")
			}
			return accountResponse(`{"code":0,"req_0":{"code":0,"data":{"info":{}}}}`), nil
		default:
			t.Fatal("unexpected QQ destination")
			return nil, nil
		}
	}))
	ctx := context.Background()
	begin, err := c.Account(ctx, online.AccountRequest{Action: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(begin.View.QRImage, "data:image/png;base64,") {
		t.Fatal("missing QR")
	}
	time.Sleep(3100 * time.Millisecond)
	done, err := c.Account(ctx, online.AccountRequest{Action: "poll", LoginID: begin.View.LoginID})
	if err != nil {
		t.Fatal(err)
	}
	if done.View.State != "signedIn" || done.View.AccountID != "12345" {
		t.Fatal("missing QQ account")
	}
}

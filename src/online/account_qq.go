package online

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const qqUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func qqHash(value string, initial uint32) string {
	for _, b := range []byte(value) {
		initial = initial*33 + uint32(b)
	}
	return strconv.FormatUint(uint64(initial&0x7fffffff), 10)
}
func (c *Client) beginQQ(ctx context.Context) error {
	q := url.Values{"appid": {"716027609"}, "e": {"2"}, "l": {"M"}, "s": {"3"}, "d": {"72"}, "v": {"4"}, "t": {strconv.FormatInt(time.Now().UnixMilli(), 10)}, "daid": {"383"}, "pt_3rd_aid": {"100497308"}}
	b, res, err := c.accountHTTP(ctx, "GET", "https://ssl.ptlogin2.qq.com/ptqrshow?"+q.Encode(), "", map[string]string{"Referer": "https://xui.ptlogin2.qq.com/", "User-Agent": qqUA}, nil)
	if err != nil {
		return err
	}
	captureCookies(res, c.account.pending.Cookies)
	if c.account.pending.Cookies["qrsig"] == "" || len(b) > 128<<10 || len(b) < 8 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return upstreamInvalid()
	}
	c.account.view.Method = "qr"
	c.account.view.QRImage = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
	return nil
}

var qqCallback = regexp.MustCompile(`^\s*ptuiCB\('([0-9]+)'\s*,\s*'[^']*'\s*,\s*'([^']*)'`)

func (c *Client) pollQQ(ctx context.Context) error {
	q := url.Values{"u1": {"https://graph.qq.com/oauth2.0/login_jump"}, "ptqrtoken": {qqHash(c.account.pending.Cookies["qrsig"], 0)}, "ptredirect": {"0"}, "h": {"1"}, "t": {"1"}, "g": {"1"}, "from_ui": {"1"}, "ptlang": {"2052"}, "action": {"0-0-" + strconv.FormatInt(time.Now().UnixMilli(), 10)}, "js_ver": {"20102616"}, "js_type": {"1"}, "pt_uistyle": {"40"}, "aid": {"716027609"}, "daid": {"383"}, "pt_3rd_aid": {"100497308"}, "has_onekey": {"1"}}
	b, _, err := c.accountHTTP(ctx, "GET", "https://ssl.ptlogin2.qq.com/ptqrlogin?"+q.Encode(), "", map[string]string{"Referer": "https://xui.ptlogin2.qq.com/", "User-Agent": qqUA}, map[string]string{"qrsig": c.account.pending.Cookies["qrsig"]})
	if err != nil {
		return err
	}
	parts := qqCallback.FindSubmatch(b)
	if len(parts) != 3 {
		return upstreamInvalid()
	}
	switch string(parts[1]) {
	case "66":
		c.account.view.State = "waiting"
	case "67":
		c.account.view.State = "scanned"
	case "65", "68":
		c.account.pending = accountSecret{}
		c.account.view = AccountView{State: "expired"}
	case "0":
		callback, err := url.Parse(string(parts[2]))
		if err != nil {
			return upstreamInvalid()
		}
		// Extract parameters; never follow the upstream URL or forward its cookies.
		if callback.Scheme != "https" || callback.Host != "ssl.ptlogin2.graph.qq.com" || callback.Path != "/check_sig" || !numericID.MatchString(callback.Query().Get("uin")) || !validAccountToken(callback.Query().Get("ptsigx")) {
			return upstreamInvalid()
		}
		if err = c.authorizeQQ(ctx, callback.Query().Get("uin"), callback.Query().Get("ptsigx")); err != nil {
			return err
		}
		c.completeAccount()
	default:
		return domain.Fail("ACCESS_DENIED", "QQ login was rejected")
	}
	return nil
}

func (c *Client) authorizeQQ(ctx context.Context, uin, sig string) error {
	q := url.Values{"uin": {uin}, "pttype": {"1"}, "service": {"ptqrlogin"}, "nodirect": {"0"}, "ptsigx": {sig}, "s_url": {"https://graph.qq.com/oauth2.0/login_jump"}, "ptlang": {"2052"}, "ptredirect": {"100"}, "aid": {"716027609"}, "daid": {"383"}, "j_later": {"0"}, "low_login_hour": {"0"}, "regmaster": {"0"}, "pt_login_type": {"3"}, "pt_aid": {"0"}, "pt_aaid": {"16"}, "pt_light": {"0"}, "pt_3rd_aid": {"100497308"}}
	_, res, err := c.accountHTTP(ctx, "GET", "https://ssl.ptlogin2.graph.qq.com/check_sig?"+q.Encode(), "", map[string]string{"Referer": "https://xui.ptlogin2.qq.com/", "User-Agent": qqUA}, nil)
	if err != nil {
		return err
	}
	cookies := map[string]string{}
	captureCookies(res, cookies)
	if cookies["p_skey"] == "" {
		return upstreamInvalid()
	}
	state := domain.NewID()
	form := url.Values{"response_type": {"code"}, "client_id": {"100497308"}, "redirect_uri": {"https://y.qq.com/portal/wx_redirect.html?login_type=1&surl=https://y.qq.com/"}, "scope": {"get_user_info,get_app_friends"}, "state": {state}, "switch": {""}, "from_ptlogin": {"1"}, "src": {"1"}, "update_auth": {"1"}, "openapi": {"1010_1030"}, "g_tk": {qqHash(cookies["p_skey"], 5381)}, "auth_time": {strconv.FormatInt(time.Now().UnixMilli(), 10)}, "ui": {domain.NewID()}}
	_, res, err = c.accountHTTP(ctx, "POST", "https://graph.qq.com/oauth2.0/authorize", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "User-Agent": qqUA}, cookies)
	if err != nil {
		return err
	}
	location, err := url.Parse(res.Header.Get("Location"))
	if err != nil || location.Scheme != "https" || location.Host != "y.qq.com" || location.Path != "/portal/wx_redirect.html" || location.Query().Get("state") != state || !validAccountToken(location.Query().Get("code")) {
		return upstreamInvalid()
	}
	var result struct {
		ID       json.Number `json:"musicid"`
		StringID string      `json:"str_musicid"`
		Key      string      `json:"musickey"`
		Name     string      `json:"nick"`
	}
	if err = c.qqAccountRPC(ctx, &c.account.pending, "QQConnectLogin.LoginServer", "QQLogin", map[string]any{"code": location.Query().Get("code")}, &result); err != nil {
		return err
	}
	id := result.StringID
	if id == "" {
		id = result.ID.String()
	}
	if !numericID.MatchString(id) || !validAccountToken(result.Key) {
		return upstreamInvalid()
	}
	c.account.pending.AccountID = id
	c.account.pending.MusicKey = result.Key
	c.account.pending.Name, err = textValue(result.Name, 512)
	if err != nil {
		return err
	}
	if c.account.pending.Name == "" {
		c.account.pending.Name = "QQ Music"
	}
	c.account.pending.Cookies = map[string]string{}
	return c.checkQQ(ctx, &c.account.pending)
}

func (c *Client) qqAccountRPC(ctx context.Context, secret *accountSecret, module, method string, param map[string]any, out any) error {
	comm := map[string]string{"ct": "19", "cv": "2201", "chid": "0", "guid": strings.ToUpper(secret.DeviceID), "g_tk": qqHash(secret.MusicKey, 5381), "tmeLoginType": "2"}
	cookies := map[string]string{}
	if secret.AccountID != "" {
		comm["uin"] = secret.AccountID
		comm["qq"] = secret.AccountID
		comm["authst"] = secret.MusicKey
		cookies["uin"] = secret.AccountID
		cookies["qm_keyst"] = secret.MusicKey
		cookies["qqmusic_key"] = secret.MusicKey
		cookies["qqmusic_uin"] = secret.AccountID
	}
	raw, _ := json.Marshal(map[string]any{"comm": comm, "req_0": map[string]any{"module": module, "method": method, "param": param}})
	b, _, err := c.accountHTTP(ctx, "POST", "https://u.y.qq.com/cgi-bin/musicu.fcg", string(raw), map[string]string{"Content-Type": "application/json", "User-Agent": qqUA, "Referer": "https://y.qq.com/"}, cookies)
	if err != nil {
		return err
	}
	var response struct {
		Code   *int `json:"code"`
		Result *struct {
			Code *int            `json:"code"`
			Data json.RawMessage `json:"data"`
		} `json:"req_0"`
	}
	if err = decodeAccount(b, &response); err != nil {
		return err
	}
	if response.Code == nil || response.Result == nil || response.Result.Code == nil {
		return upstreamInvalid()
	}
	for _, code := range []int{*response.Code, *response.Result.Code} {
		if code == 1000 || code == 104401 || code == 104400 {
			return domain.Fail("AUTH_REQUIRED", "QQ Music account authorization has expired")
		}
		if code != 0 {
			return domain.Fail("ACCESS_DENIED", fmt.Sprintf("QQ Music account request was rejected (%d)", code))
		}
	}
	return decodeAccount(response.Result.Data, out)
}
func (c *Client) checkQQ(ctx context.Context, secret *accountSecret) error {
	var result map[string]any
	if err := c.qqAccountRPC(ctx, secret, "music.UserInfo.userInfoServer", "GetLoginUserInfo", map[string]any{}, &result); err != nil {
		return err
	}
	if len(result) == 0 {
		return upstreamInvalid()
	}
	return nil
}

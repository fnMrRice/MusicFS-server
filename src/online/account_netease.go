package online

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// EAPI is the platform's wire encoding, not credential storage encryption.
// Protocol reference: api-enhanced util/crypto.js and util/request.js (MIT).
func (c *Client) neteaseAccountRequest(ctx context.Context, secret *accountSecret, path string, data map[string]any, out any) error {
	header := map[string]string{"os": "pc", "appver": "3.1.17.204416", "osver": "Microsoft-Windows-10-Professional-build-19045-64bit", "channel": "netease", "deviceId": secret.DeviceID, "requestId": fmt.Sprintf("%d_0001", time.Now().UnixMilli()), "__csrf": secret.Cookies["__csrf"]}
	for _, k := range []string{"MUSIC_U", "MUSIC_A", "NMTID"} {
		if secret.Cookies[k] != "" {
			header[k] = secret.Cookies[k]
		}
	}
	data["header"] = header
	data["e_r"] = false
	raw, err := json.Marshal(data)
	if err != nil {
		return upstreamInvalid()
	}
	hash := md5.Sum([]byte("nobody" + path + "use" + string(raw) + "md5forencrypt"))
	payload := []byte(path + "-36cd479b6b5-" + string(raw) + "-36cd479b6b5-" + hex.EncodeToString(hash[:]))
	pad := aes.BlockSize - len(payload)%aes.BlockSize
	payload = append(payload, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher([]byte("e82ckenh8dichen8"))
	for i := 0; i < len(payload); i += aes.BlockSize {
		block.Encrypt(payload[i:i+aes.BlockSize], payload[i:i+aes.BlockSize])
	}
	form := url.Values{"params": {strings.ToUpper(hex.EncodeToString(payload))}}
	body, res, err := c.accountHTTP(ctx, "POST", "https://interface.music.163.com/eapi/"+strings.TrimPrefix(path, "/api/"), form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "User-Agent": "Mozilla/5.0 (Windows NT 10.0; WOW64) NeteaseMusicDesktop/3.1.17.204416"}, header)
	if err != nil {
		return err
	}
	captureCookies(res, secret.Cookies)
	return decodeAccount(body, out)
}

func (c *Client) beginNetease(ctx context.Context) error {
	var result struct {
		Code int    `json:"code"`
		Key  string `json:"unikey"`
	}
	if err := c.neteaseAccountRequest(ctx, &c.account.pending, "/api/login/qrcode/unikey", map[string]any{"type": 3}, &result); err != nil {
		return err
	}
	if err := c.businessError(result.Code, 200); err != nil {
		return err
	}
	if !validAccountToken(result.Key) {
		return upstreamInvalid()
	}
	c.account.key = result.Key
	c.account.view.Method = "qr"
	c.account.view.QRURL = "https://music.163.com/login?codekey=" + url.QueryEscape(result.Key)
	return nil
}
func (c *Client) pollNetease(ctx context.Context) error {
	var result struct {
		Code int `json:"code"`
	}
	if err := c.neteaseAccountRequest(ctx, &c.account.pending, "/api/login/qrcode/client/login", map[string]any{"type": 3, "key": c.account.key}, &result); err != nil {
		return err
	}
	switch result.Code {
	case 800:
		c.account.view = AccountView{State: "expired"}
		c.account.pending = accountSecret{}
		c.account.key = ""
	case 801:
		c.account.view.State = "waiting"
	case 802:
		c.account.view.State = "scanned"
	case 803:
		if c.account.pending.Cookies["MUSIC_U"] == "" {
			return upstreamInvalid()
		}
		if err := c.checkNetease(ctx, &c.account.pending); err != nil {
			return err
		}
		c.completeAccount()
	default:
		return c.businessError(result.Code, 803)
	}
	return nil
}
func (c *Client) checkNetease(ctx context.Context, secret *accountSecret) error {
	var result struct {
		Code    int `json:"code"`
		Profile *struct {
			ID        json.Number `json:"userId"`
			Name      string      `json:"nickname"`
			AvatarURL string      `json:"avatarUrl"`
		} `json:"profile"`
	}
	if err := c.neteaseAccountRequest(ctx, secret, "/api/nuser/account/get", map[string]any{}, &result); err != nil {
		return err
	}
	if err := c.businessError(result.Code, 200); err != nil {
		return err
	}
	if result.Profile == nil || !numericID.MatchString(result.Profile.ID.String()) {
		return domain.Fail("AUTH_REQUIRED", "Platform account authorization has expired")
	}
	name, err := textValue(result.Profile.Name, 512)
	if err != nil {
		return err
	}
	if secret.AccountID != "" && secret.AccountID != result.Profile.ID.String() {
		return domain.Fail("ACCESS_DENIED", "Platform account identity changed")
	}
	secret.AccountID = result.Profile.ID.String()
	secret.Name = name
	secret.AvatarURL = accountAvatarURL(Netease, result.Profile.AvatarURL)
	return nil
}

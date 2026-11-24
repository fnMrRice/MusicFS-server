package driver

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

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/session"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// EAPI is the platform's wire encoding, not credential storage encryption.
// Protocol reference: api-enhanced util/crypto.js and util/request.js (MIT).
func (c *Client) Request(ctx context.Context, secret *session.Secret, path string, data map[string]any, out any) error {
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
	body, res, err := c.HTTP(ctx, "POST", "https://interface.music.163.com/eapi/"+strings.TrimPrefix(path, "/api/"), form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "User-Agent": "Mozilla/5.0 (Windows NT 10.0; WOW64) NeteaseMusicDesktop/3.1.17.204416"}, header)
	if err != nil {
		return err
	}
	captureCookies(res, secret.Cookies)
	return decodeAccount(body, out)
}

func (c *Client) Begin(ctx context.Context, _ online.AccountRequest) error {
	var result struct {
		Code int    `json:"code"`
		Key  string `json:"unikey"`
	}
	if err := c.Request(ctx, c.Pending, "/api/login/qrcode/unikey", map[string]any{"type": 3}, &result); err != nil {
		return err
	}
	if err := c.BusinessError(result.Code, 200); err != nil {
		return err
	}
	if !session.ValidToken(result.Key) {
		return upstreamInvalid()
	}
	*c.Key = result.Key
	c.View.Method = "qr"
	c.View.QRURL = "https://music.163.com/login?codekey=" + url.QueryEscape(result.Key)
	return nil
}
func (c *Client) Poll(ctx context.Context, _ online.AccountRequest) error {
	var result struct {
		Code int `json:"code"`
	}
	if err := c.Request(ctx, c.Pending, "/api/login/qrcode/client/login", map[string]any{"type": 3, "key": *c.Key}, &result); err != nil {
		return err
	}
	switch result.Code {
	case 800:
		*c.View = online.AccountView{State: "expired"}
		*c.Pending = session.Secret{}
		*c.Key = ""
	case 801:
		c.View.State = "waiting"
	case 802:
		c.View.State = "scanned"
	case 803:
		if c.Pending.Cookies["MUSIC_U"] == "" {
			return upstreamInvalid()
		}
		if err := c.Check(ctx, c.Pending); err != nil {
			return err
		}
		c.Complete()
	default:
		return c.BusinessError(result.Code, 803)
	}
	return nil
}
func (c *Client) Check(ctx context.Context, secret *session.Secret) error {
	var result struct {
		Code    int `json:"code"`
		Profile *struct {
			ID        json.Number `json:"userId"`
			Name      string      `json:"nickname"`
			AvatarURL string      `json:"avatarUrl"`
			membershipFields
		} `json:"profile"`
	}
	if err := c.Request(ctx, secret, "/api/nuser/account/get", map[string]any{}, &result); err != nil {
		return err
	}
	if err := c.BusinessError(result.Code, 200); err != nil {
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
	secret.AvatarURL = c.AvatarURL(result.Profile.AvatarURL)
	secret.MembershipTier = c.membershipTier(ctx, secret.AccountID, result.Profile.membershipFields)
	return nil
}

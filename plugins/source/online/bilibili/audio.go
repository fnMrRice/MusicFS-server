package bilibili

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type audioChoice struct {
	url      string
	bitrate  int64
	size     int64
	duration *int64
}
type dashAudio struct {
	URL           string `json:"baseUrl"`
	Alternate     string `json:"base_url"`
	MIME          string `json:"mimeType"`
	AlternateMIME string `json:"mime_type"`
	Codecs        string `json:"codecs"`
	Bandwidth     int64  `json:"bandwidth"`
}

func (c *Client) chooseAudio(ctx context.Context, r reference) (audioChoice, error) {
	quality := provider.PlaybackQuality(ctx)
	if quality != "" && quality != "highest" && quality != "lowest" {
		return audioChoice{}, fault.Fail("INVALID_ARGUMENT", "Unknown Bilibili audio quality")
	}
	if r.kind == "audio" {
		info, err := c.audio(ctx, r)
		if err != nil {
			return audioChoice{}, err
		}
		if info.Limit != 0 {
			return audioChoice{}, fault.Fail("ACCESS_DENIED", "Bilibili audio is restricted")
		}
		var result struct {
			URLs []string `json:"cdns"`
			Size int64    `json:"size"`
			Type int      `json:"type"`
		}
		if err := c.get(ctx, audioRoot+"/url", url.Values{"sid": {r.id}, "privilege": {"2"}, "quality": {"2"}}, &result); err != nil {
			return audioChoice{}, err
		}
		if result.Size < 0 || result.Type != 1 {
			return audioChoice{}, fault.Fail("UNSUPPORTED", "Bilibili audio format is not supported")
		}
		for _, raw := range result.URLs {
			u, err := url.Parse(raw)
			if err == nil && safeCDN(raw) && strings.HasSuffix(strings.ToLower(u.Path), ".m4a") {
				choice := audioChoice{url: raw, size: result.Size}
				if info.Duration > 0 {
					ms := info.Duration * 1000
					choice.duration = &ms
				}
				return choice, nil
			}
		}
		return audioChoice{}, fault.Fail("UNSUPPORTED", "Bilibili did not provide an allowed HTTPS M4A resource")
	}
	if r.kind != "video" {
		return audioChoice{}, fault.Fail("INVALID_ARGUMENT", "A Bilibili track reference is required")
	}
	info, err := c.video(ctx, r)
	if err != nil {
		return audioChoice{}, err
	}
	part, err := selectedPage(info, r)
	if err != nil {
		return audioChoice{}, err
	}
	if info.Rights.Pay != 0 || info.Rights.UGCPay != 0 || info.Rights.Preview != 0 || info.Rights.ArcPay != 0 || info.Rights.NoBackground != 0 || info.Exclusive || info.Preview {
		return audioChoice{}, fault.Fail("ACCESS_DENIED", "Bilibili video does not allow complete anonymous audio playback")
	}
	var result struct {
		TimeLength int64 `json:"timelength"`
		Preview    int   `json:"is_preview"`
		Dash       struct {
			Audio []dashAudio `json:"audio"`
		} `json:"dash"`
	}
	if err := c.get(ctx, apiRoot+"/x/player/playurl", url.Values{"bvid": {info.BVID}, "cid": {strconv.FormatInt(part.CID, 10)}, "fnval": {"16"}, "fnver": {"0"}, "fourk": {"0"}}, &result); err != nil {
		return audioChoice{}, err
	}
	if result.Preview != 0 || result.TimeLength < part.Duration*1000-2000 {
		return audioChoice{}, fault.Fail("ACCESS_DENIED", "Bilibili returned only a preview")
	}
	best := audioChoice{}
	for _, audio := range result.Dash.Audio {
		if audio.URL == "" {
			audio.URL = audio.Alternate
		}
		if audio.MIME == "" {
			audio.MIME = audio.AlternateMIME
		}
		if audio.MIME != "audio/mp4" || audio.Codecs != "mp4a.40.2" || audio.Bandwidth < 1 || !safeCDN(audio.URL) {
			continue
		}
		if best.url == "" || quality == "lowest" && audio.Bandwidth < best.bitrate || quality != "lowest" && audio.Bandwidth > best.bitrate {
			best = audioChoice{url: audio.URL, bitrate: audio.Bandwidth, duration: &result.TimeLength}
		}
	}
	if best.url == "" {
		return best, fault.Fail("UNSUPPORTED", "Bilibili did not return a supported independent AAC audio track")
	}
	return best, nil
}

func audioReference(id string) (reference, error) {
	r, err := parseReference(id)
	if err != nil {
		return r, err
	}
	if r.kind != "audio" && (r.kind != "video" || r.cid == "") || r.canonical() != id {
		return r, fault.Fail("INVALID_ARGUMENT", "Audio playback requires a canonical track identity")
	}
	return r, nil
}

func audioDescription(id string, choice audioChoice) pluginproto.OnlineAudio {
	size := choice.size
	if size == 0 {
		size = -1
	}
	audio := pluginproto.OnlineAudio{ExternalID: id, MIME: "audio/mp4", Codec: "aac", Container: "m4a", SizeBytes: size, DurationMs: choice.duration}
	if choice.bitrate > 0 {
		audio.BitrateBps = &choice.bitrate
	}
	return audio
}

// DescribeAudio 只读取平台 JSON，用于播放格式协商；不会访问 CDN 或读取音频。
func (c *Client) DescribeAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, error) {
	r, err := audioReference(id)
	if err != nil {
		return pluginproto.OnlineAudio{}, err
	}
	choice, err := c.chooseAudio(ctx, r)
	if err != nil {
		return pluginproto.OnlineAudio{}, err
	}
	audio := audioDescription(id, choice)
	return audio, audio.Validate()
}

// Resolve 保留 Provider 能力入口，只检查播放描述，不打开音频资源。
func (c *Client) Resolve(ctx context.Context, id string) error {
	_, err := c.DescribeAudio(ctx, id)
	return err
}

// OpenAudio 仅在宿主收到播放请求后执行；不预读、预下载或散列远端内容。
func (c *Client) OpenAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	var empty pluginproto.OnlineAudio
	r, err := audioReference(id)
	if err != nil {
		return empty, nil, err
	}
	choice, err := c.chooseAudio(ctx, r)
	if err != nil {
		return empty, nil, err
	}
	res, err := c.openCDN(ctx, choice.url)
	if err != nil {
		return empty, nil, err
	}
	size := res.ContentLength
	if size > pluginproto.MaxOnlineAudioBytes || choice.size > pluginproto.MaxOnlineAudioBytes || size > 0 && choice.size > 0 && size != choice.size || res.Header.Get("Content-Range") != "" || res.Header.Get("Content-Encoding") != "" {
		res.Body.Close()
		return empty, nil, fault.Fail("UNSUPPORTED", "Bilibili did not return a complete bounded audio response")
	}
	if size < 1 && choice.size > 0 {
		size = choice.size
	}
	audio := audioDescription(id, choice)
	audio.SizeBytes = size
	tag := res.Header.Get("ETag")
	if len(tag) > 2 && len(tag) <= 1024 && tag[0] == '"' && tag[len(tag)-1] == '"' && res.Request != nil {
		// 强 ETag 仅在对应媒体资源内有效，忽略每次读取变化的签名查询参数。
		resource := res.Request.URL
		sum := sha256.Sum256([]byte(resource.Host + "\x00" + resource.EscapedPath() + "\x00" + tag))
		audio.Version = hex.EncodeToString(sum[:])
	}
	if err := audio.Validate(); err != nil {
		res.Body.Close()
		return empty, nil, err
	}
	return audio, res.Body, nil
}

func (c *Client) openCDN(ctx context.Context, raw string) (*http.Response, error) {
	streamClient := *c.http
	streamClient.Timeout = 0
	for hop := 0; hop < 4; hop++ {
		if !safeCDN(raw) {
			return nil, fault.Fail("ACCESS_DENIED", "Bilibili audio destination is not allowed")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, online.UpstreamInvalid()
		}
		req.Header.Set("Referer", "https://www.bilibili.com/")
		req.Header.Set("User-Agent", "Mozilla/5.0 MusicFS/0.1")
		req.Header.Set("Accept-Encoding", "identity")
		res, err := streamClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fault.Fail("TEMPORARY_FAILURE", "Bilibili audio request failed")
		}
		if res.StatusCode == http.StatusOK {
			if res.Request == nil {
				res.Request = req
			}
			return res, nil
		}
		res.Body.Close()
		if res.StatusCode != 301 && res.StatusCode != 302 && res.StatusCode != 303 && res.StatusCode != 307 && res.StatusCode != 308 {
			return nil, statusError(res)
		}
		next, err := res.Location()
		if err != nil {
			return nil, online.UpstreamInvalid()
		}
		raw = next.String()
	}
	return nil, fault.Fail("ACCESS_DENIED", "Bilibili audio redirect limit exceeded")
}

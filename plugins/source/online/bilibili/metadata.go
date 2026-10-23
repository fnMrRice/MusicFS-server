package bilibili

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type videoPage struct {
	CID      int64  `json:"cid"`
	Page     int    `json:"page"`
	Part     string `json:"part"`
	Duration int64  `json:"duration"`
}
type videoInfo struct {
	BVID  string `json:"bvid"`
	AID   int64  `json:"aid"`
	Title string `json:"title"`
	State int    `json:"state"`
	Owner struct {
		MID  int64  `json:"mid"`
		Name string `json:"name"`
	} `json:"owner"`
	Pages  []videoPage `json:"pages"`
	Rights struct {
		Pay          int `json:"pay"`
		UGCPay       int `json:"ugc_pay"`
		Preview      int `json:"ugc_pay_preview"`
		ArcPay       int `json:"arc_pay"`
		NoBackground int `json:"no_background"`
	} `json:"rights"`
	Exclusive bool `json:"is_upower_exclusive"`
	Preview   bool `json:"is_upower_preview"`
}
type audioInfo struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	Uname    string `json:"uname"`
	UID      int64  `json:"uid"`
	Duration int64  `json:"duration"`
	Limit    int    `json:"limit"`
}

func (c *Client) video(ctx context.Context, r reference) (videoInfo, error) {
	q := url.Values{}
	if bv.MatchString(r.id) {
		q.Set("bvid", r.id)
	} else {
		q.Set("aid", r.id)
	}
	var info videoInfo
	if err := c.get(ctx, apiRoot+"/x/web-interface/view", q, &info); err != nil {
		return info, err
	}
	if !bv.MatchString(info.BVID) || len(info.Pages) == 0 || len(info.Pages) > online.MaxPlaylistEntries || (bv.MatchString(r.id) && info.BVID != r.id) || (!bv.MatchString(r.id) && strconv.FormatInt(info.AID, 10) != r.id) {
		return info, online.UpstreamInvalid()
	}
	if info.State < 0 {
		return info, fault.Fail("NOT_FOUND", "Bilibili video is unavailable")
	}
	seen := map[int64]bool{}
	for i, page := range info.Pages {
		if page.CID <= 0 || page.Page != i+1 || seen[page.CID] {
			return info, online.UpstreamInvalid()
		}
		seen[page.CID] = true
	}
	return info, nil
}

func selectedPage(info videoInfo, r reference) (videoPage, error) {
	for _, page := range info.Pages {
		if (r.cid != "" && strconv.FormatInt(page.CID, 10) == r.cid) || (r.cid == "" && page.Page == r.page) {
			return page, nil
		}
	}
	return videoPage{}, fault.Fail("NOT_FOUND", "Bilibili video part was not found")
}

func videoTrack(info videoInfo, page videoPage) (online.Track, error) {
	title := info.Title
	if len(info.Pages) > 1 {
		title += " · P" + strconv.Itoa(page.Page) + " " + page.Part
	}
	return makeTrack("video:"+info.BVID+":"+strconv.FormatInt(page.CID, 10), title, "", page.Duration)
}

func makeTrack(id, title, artist string, duration int64) (online.Track, error) {
	title, err := online.TextValue(title, 4096)
	if err != nil || title == "" || duration < 0 || duration > 7*24*60*60 {
		return online.Track{}, online.UpstreamInvalid()
	}
	t := online.Track{ExternalID: id, Title: title, Artists: []string{}, Album: "", SourceURL: sourceURL(id), PlaybackAvailability: "unknown"}
	if artist != "" {
		value, err := online.TextValue(artist, 4096)
		if err != nil {
			return t, err
		}
		if value != "" {
			t.Artists = append(t.Artists, value)
		}
	}
	if duration > 0 {
		ms := duration * 1000
		t.DurationMs = &ms
	}
	t.MetadataDigest = online.Digest(t)
	return t, nil
}

func (c *Client) audio(ctx context.Context, r reference) (audioInfo, error) {
	var info audioInfo
	if err := c.get(ctx, audioRoot+"/song/info", url.Values{"sid": {r.id}}, &info); err != nil {
		return info, err
	}
	if strconv.FormatInt(info.ID, 10) != r.id {
		return info, online.UpstreamInvalid()
	}
	return info, nil
}

func (c *Client) Metadata(ctx context.Context, raw string) (online.Track, error) {
	if strings.HasPrefix(raw, "unavailable:") && online.BilibiliEntryID(raw) {
		return online.Track{}, fault.Fail("NOT_FOUND", "Bilibili collection item is unavailable")
	}
	r, err := parseReference(raw)
	if err != nil {
		return online.Track{}, err
	}
	switch r.kind {
	case "video":
		info, err := c.video(ctx, r)
		if err != nil {
			return online.Track{}, err
		}
		page, err := selectedPage(info, r)
		if err != nil {
			return online.Track{}, err
		}
		return videoTrack(info, page)
	case "audio":
		info, err := c.audio(ctx, r)
		if err != nil {
			return online.Track{}, err
		}
		return makeTrack("audio:"+r.id, info.Title, info.Author, info.Duration)
	default:
		return online.Track{}, fault.Fail("INVALID_ARGUMENT", "A Bilibili track reference is required")
	}
}

func (c *Client) TrackDetails(ctx context.Context, raw string) (pluginproto.TrackDetails, error) {
	r, err := parseReference(raw)
	if err != nil {
		return pluginproto.TrackDetails{}, err
	}
	var track online.Track
	fields := []pluginproto.TrackDetailField{}
	if r.kind == "video" {
		info, err := c.video(ctx, r)
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		page, err := selectedPage(info, r)
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		track, err = videoTrack(info, page)
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		fields = append(fields, pluginproto.TrackDetailField{ID: "bvid", Label: "BV 号", Value: info.BVID}, pluginproto.TrackDetailField{ID: "cid", Label: "分 P 身份", Value: strconv.FormatInt(page.CID, 10)}, pluginproto.TrackDetailField{ID: "part", Label: "分 P 序号", Value: strconv.Itoa(page.Page)})
		if info.Owner.Name != "" {
			fields = append(fields, pluginproto.TrackDetailField{ID: "uploader", Label: "上传者", Value: info.Owner.Name})
		}
		if info.Owner.MID > 0 {
			fields = append(fields, pluginproto.TrackDetailField{ID: "uploaderId", Label: "上传者 UID", Value: strconv.FormatInt(info.Owner.MID, 10)})
		}
	} else if r.kind == "audio" {
		info, err := c.audio(ctx, r)
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		track, err = makeTrack("audio:"+r.id, info.Title, info.Author, info.Duration)
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		fields = append(fields, pluginproto.TrackDetailField{ID: "audioId", Label: "音频区编号", Value: "au" + r.id})
		if info.Uname != "" {
			fields = append(fields, pluginproto.TrackDetailField{ID: "uploader", Label: "上传者", Value: info.Uname})
		}
	} else {
		return pluginproto.TrackDetails{}, fault.Fail("INVALID_ARGUMENT", "A Bilibili track reference is required")
	}
	fields = append(fields, pluginproto.TrackDetailField{ID: "source", Label: "来源页面", Value: track.SourceURL})
	d := pluginproto.TrackDetails{ExternalID: raw, Title: track.Title, Sections: []pluginproto.TrackDetailSection{{ID: "source", Title: "Bilibili 来源", Fields: fields}}}
	return d, d.Validate(raw)
}

func (c *Client) MetadataBatch(ctx context.Context, ids []string) (online.TrackBatch, error) {
	result := online.TrackBatch{Items: []online.TrackLookup{}}
	if len(ids) == 0 || len(ids) > online.MaxPageItems {
		return result, fault.Fail("INVALID_ARGUMENT", "Track batch size must be between 1 and 50")
	}
	for _, id := range ids {
		track, err := c.Metadata(ctx, id)
		item := online.TrackLookup{ExternalID: id}
		if err != nil {
			var f *fault.Error
			if !errors.As(err, &f) || f.Code != "NOT_FOUND" {
				return result, err
			}
			code := f.Code
			item.ErrorCode = &code
		} else {
			if track.ExternalID != id {
				return result, fault.Fail("INVALID_ARGUMENT", "Batch lookups require canonical track identities")
			}
			item.Track = &track
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

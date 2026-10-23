package bilibili

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

type snapshot struct {
	observation online.PlaylistObservation
	tracks      []online.TrackLookup
	expires     time.Time
}
type favoritePage struct {
	Info struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Count int    `json:"media_count"`
		Attr  *int   `json:"attr"`
		Upper struct {
			Name string `json:"name"`
		} `json:"upper"`
	} `json:"info"`
	Medias []struct {
		ID   int64  `json:"id"`
		BVID string `json:"bvid"`
		Type int    `json:"type"`
	} `json:"medias"`
	HasMore bool `json:"has_more"`
}

func playlist(id, title, creator string) (online.Playlist, error) {
	title, err := online.TextValue(title, 4096)
	if err != nil || title == "" {
		return online.Playlist{}, online.UpstreamInvalid()
	}
	p := online.Playlist{ExternalID: id, Title: title, SourceURL: sourceURL(id)}
	if creator != "" {
		value, err := online.TextValue(creator, 4096)
		if err != nil {
			return p, err
		}
		if value != "" {
			p.Creator = &value
		}
	}
	return p, nil
}

func appendTrack(s *snapshot, track online.Track) {
	s.observation.Entries = append(s.observation.Entries, online.PlaylistEntry{Position: len(s.tracks), ExternalTrackID: track.ExternalID})
	s.tracks = append(s.tracks, online.TrackLookup{ExternalID: track.ExternalID, Track: &track})
}

func appendUnavailable(s *snapshot, id string) {
	s.observation.Entries = append(s.observation.Entries, online.PlaylistEntry{Position: len(s.tracks), ExternalTrackID: id})
	code := "NOT_FOUND"
	s.tracks = append(s.tracks, online.TrackLookup{ExternalID: id, ErrorCode: &code})
}

func (c *Client) favorites(ctx context.Context, r reference) (snapshot, error) {
	s := snapshot{observation: online.PlaylistObservation{Entries: []online.PlaylistEntry{}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}, tracks: []online.TrackLookup{}}
	videoCache := map[string]videoInfo{}
	seenMedia := 0
	expected := -1
	for pn := 1; pn <= 500; pn++ {
		var page favoritePage
		if err := c.get(ctx, apiRoot+"/x/v3/fav/resource/list", url.Values{"media_id": {r.id}, "pn": {strconv.Itoa(pn)}, "ps": {"20"}, "order": {"mtime"}, "type": {"0"}}, &page); err != nil {
			return s, err
		}
		if strconv.FormatInt(page.Info.ID, 10) != r.id || page.Info.Count < 0 || len(page.Medias) > 20 {
			return s, online.UpstreamInvalid()
		}
		if page.Info.Attr != nil && *page.Info.Attr&1 != 0 {
			return s, fault.Fail("ACCESS_DENIED", "Bilibili favorites are private")
		}
		if pn == 1 {
			var err error
			s.observation.Playlist, err = playlist(r.canonical(), page.Info.Title, page.Info.Upper.Name)
			if err != nil {
				return s, err
			}
			expected = page.Info.Count
		} else if expected != page.Info.Count {
			return s, fault.Fail("TEMPORARY_FAILURE", "Bilibili favorites changed during observation")
		}
		for _, item := range page.Medias {
			if len(s.tracks) >= online.MaxPlaylistEntries {
				return finishSnapshot(s, false), nil
			}
			if item.ID <= 0 || item.Type < 0 || item.Type > 99999 {
				return s, online.UpstreamInvalid()
			}
			seenMedia++
			unavailable := "unavailable:" + strconv.Itoa(item.Type) + ":" + strconv.FormatInt(item.ID, 10)
			if item.Type != 2 || !bv.MatchString(item.BVID) {
				appendUnavailable(&s, unavailable)
				continue
			}
			info, cached := videoCache[item.BVID]
			if !cached {
				var err error
				info, err = c.video(ctx, reference{kind: "video", id: item.BVID, page: 1})
				if err != nil {
					var f *fault.Error
					if errors.As(err, &f) && f.Code == "NOT_FOUND" {
						appendUnavailable(&s, unavailable)
						continue
					}
					return s, err
				}
				videoCache[item.BVID] = info
			}
			for _, part := range info.Pages {
				if len(s.tracks) >= online.MaxPlaylistEntries {
					return finishSnapshot(s, false), nil
				}
				track, err := videoTrack(info, part)
				if err != nil {
					return s, err
				}
				appendTrack(&s, track)
			}
		}
		if !page.HasMore {
			if seenMedia != expected {
				return s, fault.Fail("TEMPORARY_FAILURE", "Bilibili favorites returned an incomplete collection")
			}
			return finishSnapshot(s, true), nil
		}
		if len(page.Medias) == 0 {
			return s, online.UpstreamInvalid()
		}
	}
	return finishSnapshot(s, false), nil
}

func (c *Client) menu(ctx context.Context, r reference) (snapshot, error) {
	s := snapshot{observation: online.PlaylistObservation{Entries: []online.PlaylistEntry{}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}, tracks: []online.TrackLookup{}}
	var info struct {
		ID    int64  `json:"menuId"`
		Title string `json:"title"`
		Uname string `json:"uname"`
	}
	if err := c.get(ctx, audioRoot+"/menu/info", url.Values{"sid": {r.id}}, &info); err != nil {
		return s, err
	}
	if strconv.FormatInt(info.ID, 10) != r.id {
		return s, online.UpstreamInvalid()
	}
	var err error
	s.observation.Playlist, err = playlist(r.canonical(), info.Title, info.Uname)
	if err != nil {
		return s, err
	}
	expected := -1
	for pn := 1; pn <= 100; pn++ {
		var page struct {
			Current   int         `json:"curPage"`
			PageCount int         `json:"pageCount"`
			Total     int         `json:"totalSize"`
			Data      []audioInfo `json:"data"`
		}
		if err := c.get(ctx, audioRoot+"/song/of-menu", url.Values{"sid": {r.id}, "pn": {strconv.Itoa(pn)}, "ps": {"100"}}, &page); err != nil {
			return s, err
		}
		if page.Current != pn || page.Total < 0 || page.PageCount < 0 || len(page.Data) > 100 {
			return s, online.UpstreamInvalid()
		}
		if pn == 1 {
			expected = page.Total
		} else if page.Total != expected {
			return s, fault.Fail("TEMPORARY_FAILURE", "Bilibili audio menu changed during observation")
		}
		for _, item := range page.Data {
			if len(s.tracks) >= online.MaxPlaylistEntries {
				return finishSnapshot(s, false), nil
			}
			if item.ID <= 0 {
				return s, online.UpstreamInvalid()
			}
			track, err := makeTrack("audio:"+strconv.FormatInt(item.ID, 10), item.Title, item.Author, item.Duration)
			if err != nil {
				return s, err
			}
			appendTrack(&s, track)
		}
		if pn >= page.PageCount {
			if len(s.tracks) != expected {
				return s, fault.Fail("TEMPORARY_FAILURE", "Bilibili audio menu returned an incomplete collection")
			}
			return finishSnapshot(s, true), nil
		}
		if len(page.Data) == 0 {
			return s, online.UpstreamInvalid()
		}
	}
	return finishSnapshot(s, false), nil
}

func finishSnapshot(s snapshot, complete bool) snapshot {
	s.observation.Complete = complete
	if complete {
		count := int64(len(s.tracks))
		s.observation.Playlist.TrackCount = &count
	} else {
		s.observation.Playlist.TrackCount = nil
	}
	s.observation.MetadataDigest = online.PlaylistDigest(s.observation.Playlist, s.observation.Entries)
	s.expires = time.Now().Add(5 * time.Minute)
	return s
}

func (c *Client) observe(ctx context.Context, raw string) (snapshot, error) {
	r, err := parseReference(raw)
	if err != nil {
		return snapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	switch r.kind {
	case "favorite":
		return c.favorites(ctx, r)
	case "audio-menu":
		return c.menu(ctx, r)
	default:
		return snapshot{}, fault.Fail("INVALID_ARGUMENT", "A Bilibili favorites or audio menu reference is required")
	}
}

func (c *Client) ObservePlaylist(ctx context.Context, id string) (online.PlaylistObservation, error) {
	s, err := c.observe(ctx, id)
	return s.observation, err
}

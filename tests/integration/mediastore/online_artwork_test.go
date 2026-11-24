package mediastore_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlineartwork"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type artTracks struct{ fixture }

func (r artTracks) Track(ctx context.Context, owner, id string) (library.Track, error) {
	return r.store.FindTrack(ctx, owner, id)
}

type artProvider struct {
	mu      sync.Mutex
	batches [][]string
}

func (*artProvider) Runtime(context.Context, string, string, string) (onlinehost.Runtime, error) {
	return onlinehost.Runtime{Platform: online.Netease}, nil
}
func (p *artProvider) MetadataBatch(_ context.Context, _, _ string, ids []string) (online.TrackBatch, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batches = append(p.batches, slices.Clone(ids))
	var out online.TrackBatch
	for _, id := range ids {
		cover := "https://p3.music.126.net/images/" + id + ".png"
		out.Items = append(out.Items, online.TrackLookup{ExternalID: id, Track: &online.Track{ExternalID: id, CoverURL: &cover}})
	}
	return out, nil
}

type artTransport func(*http.Request) (*http.Response, error)

func (f artTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnlineArtworkRecoversOldSourcesBatchesAndProtectsManualCover(t *testing.T) {
	ctx, f := t.Context(), setup(t)
	if err := f.store.BindMusicSourcePlugins(ctx, map[string]string{"net": pluginproto.OnlinePluginID(online.Netease)}); err != nil {
		t.Fatal(err)
	}
	provider := &artProvider{}
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 7, 9))); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	service := onlineartwork.New(artTracks{f}, provider, f.service, artTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "p3.music.126.net" || r.URL.Scheme != "https" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			return nil, errors.New("unexpected CDN request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(body.Bytes()))}, nil
	}))
	var tracks []library.Track
	for _, id := range []string{"123", "456"} {
		track, err := f.store.PublishOnlineTrack(ctx, "admin", "net", online.Track{ExternalID: id, Title: "Song " + id, Artists: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.store.DB.Exec("UPDATE tracks SET document=json_remove(document,'$._providerCoverUrl') WHERE id=?", track.ID); err != nil {
			t.Fatal(err)
		}
		track, err = f.store.FindTrack(ctx, "admin", track.ID)
		if err != nil || track.OnlineCoverPath == "" || track.CoverObjectID != nil {
			t.Fatalf("legacy cover discovery failed: %+v %v", track, err)
		}
		tracks = append(tracks, track)
	}
	var wg sync.WaitGroup
	for _, track := range tracks {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			o, r, err := service.Open(ctx, "admin", id)
			if err != nil {
				t.Error(err)
				return
			}
			defer r.Close()
			data, err := io.ReadAll(r)
			if err != nil || !bytes.Equal(data, body.Bytes()) || o.Width != 7 || o.Height != 9 {
				t.Error("artwork bytes or verified dimensions changed", err)
			}
		}(track.ID)
	}
	wg.Wait()
	if len(provider.batches) != 1 || len(provider.batches[0]) != 2 || calls.Load() != 2 {
		t.Fatalf("unexpected CDN or platform request totals: %v, %d", provider.batches, calls.Load())
	}
	first, err := f.store.FindTrack(ctx, "admin", tracks[0].ID)
	if err != nil || first.CoverObjectID == nil || first.OnlineCoverPath != "" {
		t.Fatal("downloaded cover did not become a normal media object", err)
	}
	if _, r, err := service.Open(ctx, "admin", first.ID); err != nil {
		t.Fatal(err)
	} else {
		r.Close()
	}
	if _, _, err := service.Open(ctx, "another", first.ID); err == nil {
		t.Fatal("another owner obtained artwork")
	}
	localID := f.metadata.Target.SourceTrackID
	meta := f.metadata
	meta.Field, meta.Language = "artwork", ""
	manual, err := f.service.Upload(ctx, "admin", meta, "image/png", domain.NewID(), bytes.NewReader(body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: meta.Target, ObjectID: manual.ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	link, err := f.store.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{localID, first.ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.SelectRecordingCover(ctx, "admin", library.SelectRecordingCoverRequest{RecordingID: *link.RecordingID, ObjectID: &manual.ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	o, r, err := service.Open(ctx, "admin", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if o.ID != manual.ID || calls.Load() != 2 {
		t.Fatal("manual recording cover superseded or cached image redownloaded")
	}
}

func TestOnlineArtworkRejectsUnsafeReferencesAndRedirects(t *testing.T) {
	ctx, f := t.Context(), setup(t)
	if err := f.store.BindMusicSourcePlugins(ctx, map[string]string{"net": pluginproto.OnlinePluginID(online.Netease)}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://p3.music.126.net/a", "https://127.0.0.1/a", "https://p3.music.126.net.evil.test/a", "https://p3.music.126.net/a?token=1", "https://u@p3.music.126.net/a", "https://p3.music.126.net:8443/a"} {
		if library.NetEaseArtworkURL(raw) != "" {
			t.Fatalf("unsafe reference accepted: %s", raw)
		}
	}
	raw := "https://p3.music.126.net/a.jpg"
	track, err := f.store.PublishOnlineTrack(ctx, "admin", "net", online.Track{ExternalID: "999", Title: "redirect", Artists: []string{}, CoverURL: &raw})
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	s := onlineartwork.New(artTracks{f}, &artProvider{}, f.service, artTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.test/private"}}, Body: http.NoBody}, nil
	}))
	if _, _, err = s.Open(ctx, "admin", track.ID); err == nil || requests != 1 {
		t.Fatal("redirect left the CDN allowlist")
	}
}

package playback_test

import (
	"bytes"
	"context"
	"encoding/json"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type networkFixture struct {
	provider.UnimplementedClient
	url   string
	opens atomic.Int32
}

func (*networkFixture) Descriptor() pluginproto.OnlineDescriptor {
	return onlineplugins.Describe(online.Bilibili)
}

func (p *networkFixture) Metadata(_ context.Context, id string) (online.Track, error) {
	duration := int64(6000)
	t := online.Track{ExternalID: id, Title: "Network fixture", Artists: []string{}, DurationMs: &duration, SourceURL: online.ReferenceURL(onlineplugins.Describe(online.Bilibili).Resources.Tracks, id), PlaybackAvailability: "unknown"}
	t.MetadataDigest = online.Digest(t)
	return t, nil
}
func (p *networkFixture) DescribeAudio(_ context.Context, id string) (pluginproto.OnlineAudio, error) {
	return pluginproto.OnlineAudio{ExternalID: id, MIME: "audio/wav", Codec: "pcm_s16le", Container: "wav", SizeBytes: -1}, nil
}
func (p *networkFixture) OpenAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	p.opens.Add(1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return pluginproto.OnlineAudio{}, nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return pluginproto.OnlineAudio{}, nil, err
	}
	description, _ := p.DescribeAudio(ctx, id)
	return description, res.Body, nil
}

type networkPlaybackFixture struct {
	api         *httptest.Server
	provider    *networkFixture
	gate        *plugincontrol.Gate
	cookie      string
	who         auth.Session
	track       library.Track
	description playback.Description
	canceled    <-chan struct{}
}

func newNetworkPlaybackFixture(t *testing.T, complete ...bool) *networkPlaybackFixture {
	t.Helper()
	wave, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	canceled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wave[:1024])
		w.(http.Flusher).Flush()
		if len(complete) > 0 && complete[0] {
			_, _ = w.Write(wave[1024:])
			return
		}
		<-r.Context().Done()
		canceled <- struct{}{}
	}))
	t.Cleanup(upstream.Close)
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	queue := jobs.New(store)
	t.Cleanup(queue.Close)
	lib := library.New(store, queue, nil, nil)
	p := &networkFixture{url: upstream.URL}
	gate := plugincontrol.NewGate(true)
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: "network-fixture", Platform: online.Bilibili, ConfigRevision: "fixture", Enabled: true, Provider: p, Gate: gate}})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.ConfigureAudio(lib); err != nil {
		t.Fatal(err)
	}
	player, err := playback.New(ctx, store, lib, nil, playback.Config{CacheRoot: t.TempDir(), MaxSessions: 8, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	sessions := auth.New(store, "fixture-token", []string{"http://client.test"}, time.Hour)
	who, cookie, err := sessions.Login(ctx, "fixture-token", "http://client.test", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	server := api.New(api.Options{Auth: sessions})
	server.RegisterOnline(host)
	server.RegisterPlayback(player)
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)
	f := &networkPlaybackFixture{api: httpServer, provider: p, gate: gate, cookie: cookie, who: who, canceled: canceled}
	response := f.request(t, http.MethodPost, "/api/v1/online-instances/network-fixture/tracks/audio:1/resolve", `{}`, nil)
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("resolve failed: %d %s", response.StatusCode, body)
	}
	if err = json.NewDecoder(response.Body).Decode(&f.track); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if f.track.Asset == nil || f.track.Asset.Version != online.Unversioned || f.track.Asset.FileSizeBytes != nil || f.track.Asset.FileSHA512 != nil {
		t.Fatalf("metadata claimed verified bytes: %+v", f.track.Asset)
	}
	raw, _ := json.Marshal(playback.CreateRequest{SourceTrackID: f.track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: `audio/wav; codecs="1"`, CanPlay: "probably"}}}, Preferences: playback.Preferences{DSPExecution: "server"}})
	response = f.request(t, http.MethodPost, "/api/v1/playback/sessions", string(raw), map[string]string{"Idempotency-Key": "online-create-once"})
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		t.Fatalf("create failed: %d %s", response.StatusCode, body)
	}
	if err = json.NewDecoder(response.Body).Decode(&f.description); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if p.opens.Load() != 0 {
		t.Fatal("metadata or session creation opened upstream audio")
	}
	if f.description.SeekMode != "none" || f.description.SeekableRange != nil || f.description.ResumeDecision != "continue" || f.description.OutputProfile.MIME != "audio/wav" {
		t.Fatalf("network source format or seek mode was inferred from its platform: %+v", f.description)
	}
	return f
}

func TestCompletedNetworkResponseKeepsPlaybackSessionAlive(t *testing.T) {
	f := newNetworkPlaybackFixture(t, true)
	response := f.request(t, http.MethodGet, f.description.StreamURL, "", nil)
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || len(data) <= 1024 {
		t.Fatal("network stream did not finish", err)
	}
	response = f.request(t, http.MethodHead, f.description.StreamURL, "", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || f.provider.opens.Load() != 1 {
		t.Fatal("completed response stopped the browser's playback session")
	}
	response = f.request(t, http.MethodDelete, "/api/v1/playback/sessions/"+f.description.SessionID, "", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal("completed response could not report playback stop")
	}
}

func (f *networkPlaybackFixture) request(t *testing.T, method, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, f.api.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: api.CookieName, Value: f.cookie})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://client.test")
	req.Header.Set("X-CSRF-Token", f.who.CSRFToken)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := f.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestNetworkPlaybackReadsOnlyWhenStreamingAndRejectsResume(t *testing.T) {
	f := newNetworkPlaybackFixture(t)
	repeated := f.request(t, http.MethodPost, "/api/v1/online-instances/network-fixture/tracks/audio:1/resolve", `{}`, nil)
	var duplicate library.Track
	if err := json.NewDecoder(repeated.Body).Decode(&duplicate); err != nil {
		t.Fatal(err)
	}
	repeated.Body.Close()
	if duplicate.ID != f.track.ID || duplicate.AssetID != f.track.AssetID || f.provider.opens.Load() != 0 {
		t.Fatal("repeated metadata resolution created new audio or source identities")
	}
	for _, method := range []string{http.MethodHead, http.MethodHead} {
		response := f.request(t, method, f.description.StreamURL, "", nil)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || f.provider.opens.Load() != 0 {
			t.Fatal("HEAD consumed or opened network audio")
		}
	}
	response := f.request(t, http.MethodGet, f.description.StreamURL, "", map[string]string{"Range": "bytes=10-"})
	response.Body.Close()
	if response.StatusCode == http.StatusOK || f.provider.opens.Load() != 0 {
		t.Fatal("unverified byte resumption reached upstream")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.api.URL+f.description.StreamURL, nil)
	req.AddCookie(&http.Cookie{Name: api.CookieName, Value: f.cookie})
	req.Header.Set("Range", "bytes=0-")
	response, err := f.api.Client().Do(req)
	if err != nil {
		t.Fatal("first bytes waited for complete upstream download", err)
	}
	defer response.Body.Close()
	first := make([]byte, 4)
	if _, err = io.ReadFull(response.Body, first); err != nil || !bytes.Equal(first, []byte("RIFF")) {
		t.Fatal("stream did not deliver the upstream first bytes", err, string(first))
	}
	if response.Header.Get("Accept-Ranges") != "none" || response.Header.Get("ETag") != "" || f.provider.opens.Load() != 1 {
		t.Fatal("stream advertised stable range identity or reopened upstream")
	}
	response.Body.Close()
	cancel()
	select {
	case <-f.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("browser cancellation did not close upstream response")
	}
	response = f.request(t, http.MethodGet, f.description.StreamURL, "", nil)
	response.Body.Close()
	if response.StatusCode == http.StatusOK || f.provider.opens.Load() != 1 {
		t.Fatal("same representation restarted a different upstream response")
	}
}

func TestNetworkPlaybackRevocationAndCSRFRemainEnforced(t *testing.T) {
	f := newNetworkPlaybackFixture(t)
	response := f.request(t, http.MethodPost, "/api/v1/online-instances/network-fixture/tracks/audio:2/resolve", `{}`, map[string]string{"X-CSRF-Token": "invalid"})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("network metadata publication bypassed CSRF")
	}
	request, _ := http.NewRequest(http.MethodGet, f.api.URL+f.description.StreamURL, nil)
	response, err := f.api.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || f.provider.opens.Load() != 0 {
		t.Fatal("unauthenticated stream reached the network")
	}
	active := f.request(t, http.MethodGet, f.description.StreamURL, "", nil)
	defer active.Body.Close()
	if active.StatusCode != http.StatusOK {
		t.Fatal("network stream did not start")
	}
	f.gate.SetAccepting(false, true)
	select {
	case <-f.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("source revocation did not close the active upstream response")
	}
	response = f.request(t, http.MethodGet, f.description.StreamURL, "", nil)
	response.Body.Close()
	if response.StatusCode == http.StatusOK || f.provider.opens.Load() != 1 {
		t.Fatal("disabled source retained audio access")
	}
}

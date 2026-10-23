package plugins_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/bilibili"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

const bilibiliContractInstance = "1463118e-e2d1-564a-ab70-05a357c1b9bf"

func bilibiliContractClient(t *testing.T) *bilibili.Client {
	t.Helper()
	transport := onlineResponse(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("anonymous fixture received credentials")
		}
		var data any
		switch r.URL.Path {
		case "/x/web-interface/view":
			data = map[string]any{"bvid": "BV13x41117TL", "aid": 8903802, "title": "Public video", "owner": map[string]any{"mid": 10, "name": "Uploader"}, "pages": []any{map[string]any{"cid": 101, "page": 1, "part": "First", "duration": 60}, map[string]any{"cid": 102, "page": 2, "part": "Second", "duration": 70}}}
		case "/x/v3/fav/resource/list":
			data = map[string]any{"info": map[string]any{"id": 99, "title": "Public favorites", "media_count": 2}, "medias": []any{map[string]any{"id": 8903802, "bvid": "BV13x41117TL", "type": 2}, map[string]any{"id": 500, "type": 2}}, "has_more": false}
		case "/audio/music-service-c/web/song/info":
			data = map[string]any{"id": 10, "title": "Public audio", "author": "Artist", "duration": 60}
		default:
			t.Fatalf("unexpected network path: %s", r.URL.Path)
		}
		body, err := json.Marshal(map[string]any{"code": 0, "data": data})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	client, err := bilibili.New(online.Scope{UserID: "owner", InstanceID: bilibiliContractInstance}, bilibili.Options{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func bilibiliMetadataContract(t *testing.T, name string, value any) {
	t.Helper()
	onlineContract(t, name, value)
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "Online"+name, body)
}

func TestBilibiliMetadataActualDTOsMatchWireAndHTTPContracts(t *testing.T) {
	client := bilibiliContractClient(t)
	ctx := context.Background()
	descriptor := bilibili.Plugin(bilibili.Options{}).Descriptor
	bilibiliMetadataContract(t, "Descriptor", descriptor)
	manifest := pluginproto.Manifest{PluginID: pluginproto.OnlinePluginID(online.Bilibili), Plugin: descriptor.Plugin, SupportsMultipleInstances: true, PluginVersion: "0.1.0", ProtocolVersion: "1.0", SourceType: pluginproto.SourceNetwork, Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(online.Bilibili), Systems: []string{"windows"}, Architectures: []string{"amd64"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}
	onlineContract(t, "Manifest", manifest)
	instance := onlinehost.Instance{InstanceID: bilibiliContractInstance, Name: "Bilibili", PluginID: manifest.PluginID, Availability: "available", OnlineDescriptor: descriptor}
	body, _ := json.Marshal(instance)
	testkit.ValidateHTTP(t, "OnlineInstance", body)
	query := online.SearchRequest{Kind: "tracks", Query: "BV13x41117TL", Limit: 1}
	page, err := client.Search(ctx, query)
	if err != nil || page.NextCursor == nil {
		t.Fatal("video lookup did not produce an observation cursor", err)
	}
	bilibiliMetadataContract(t, "SearchPage", page)
	query.Cursor = *page.NextCursor
	page, err = client.Search(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	bilibiliMetadataContract(t, "SearchPage", page)
	for _, id := range []string{"video:BV13x41117TL:101", "audio:10"} {
		track, err := client.Metadata(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		bilibiliMetadataContract(t, "Track", track)
	}
	observation, err := client.ObservePlaylist(ctx, "favorite:99")
	if err != nil || len(observation.Entries) != 3 || observation.Entries[2].ExternalTrackID != "unavailable:2:500" {
		t.Fatal("collection did not retain its unavailable item", err)
	}
	bilibiliMetadataContract(t, "Observation", observation)
	request := online.PlaylistTracksRequest{ExternalID: "favorite:99", Limit: 2}
	tracks, err := client.PlaylistTracks(ctx, request)
	if err != nil || tracks.NextCursor == nil {
		t.Fatal("collection did not produce a cursor", err)
	}
	bilibiliMetadataContract(t, "PlaylistTracksPage", tracks)
	request.Cursor = *tracks.NextCursor
	tracks, err = client.PlaylistTracks(ctx, request)
	if err != nil || len(tracks.Items) != 1 || tracks.Items[0].ErrorCode == nil {
		t.Fatal("missing collection item was discarded", err)
	}
	bilibiliMetadataContract(t, "PlaylistTracksPage", tracks)
	ids := []string{"audio:10", "unavailable:2:500", "video:BV13x41117TL:101"}
	batch, err := client.MetadataBatch(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	bilibiliMetadataContract(t, "TrackBatch", batch)
	scope := pluginproto.Context{InstanceID: bilibiliContractInstance, UserID: "owner", TaskID: strings.Repeat("a", 48), TimeoutMs: 10000}
	onlineContract(t, "TrackBatchRequest", pluginproto.OnlineTrackBatch{Context: scope, ExternalIDs: ids})
	onlineContract(t, "PlaylistTracksRequest", pluginproto.OnlinePlaylistTracks{Context: scope, PlaylistTracksRequest: request})
	for _, id := range []string{"favorite:99", "audio-menu:99"} {
		body, _ := json.Marshal(playlists.RemoteRef{InstanceID: bilibiliContractInstance, ExternalID: id})
		testkit.ValidateHTTP(t, "ImportRemotePlaylistRequest", body)
	}
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "risk", "error": pluginproto.Failure("RISK_CONTROL", "Upstream risk control rejected the request")})
}

func TestBilibiliMetadataContractsRejectSpoofedScopeAndPlayablePlaceholders(t *testing.T) {
	descriptor := bilibili.Plugin(bilibili.Options{}).Descriptor
	for _, platform := range []string{online.Bilibili, online.Netease} {
		descriptor.Platform = platform
		descriptor.Authentication = "anonymous"
		if platform == online.Bilibili {
			descriptor.Authentication = "platformAccountRequired"
		}
		body, _ := json.Marshal(descriptor)
		for _, schema := range []struct{ family, name string }{{"wire", "Descriptor"}, {"http", "OnlineDescriptor"}} {
			var validationErr error
			if schema.family == "http" {
				validationErr = testkit.ContractSchema(t, "http", schema.name).Validate(testkit.DecodeJSON(t, body))
			} else {
				validationErr = onlineSchema(t, schema.name).Validate(testkit.DecodeJSON(t, body))
			}
			if validationErr == nil {
				t.Fatal("platform accepted an invalid authentication mode", platform, schema.family)
			}
		}
	}
	client := bilibiliContractClient(t)
	track, err := client.Metadata(context.Background(), "audio:10")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"https://www.bilibili.com.evil.invalid/audio/au10", "https://www.bilibili.com@evil.invalid/audio/au10", "http://127.0.0.1/private"} {
		track.SourceURL = source
		body, _ := json.Marshal(track)
		if onlineSchema(t, "Track").Validate(testkit.DecodeJSON(t, body)) == nil || testkit.ContractSchema(t, "http", "OnlineTrack").Validate(testkit.DecodeJSON(t, body)) == nil {
			t.Fatal("forged source URL accepted by contract", source)
		}
	}
	track.SourceURL = "https://www.bilibili.com/audio/au10"
	lookup := online.TrackLookup{ExternalID: "unavailable:2:500", Track: &track}
	body, _ := json.Marshal(lookup)
	if onlineSchema(t, "TrackLookup").Validate(testkit.DecodeJSON(t, body)) == nil || testkit.ContractSchema(t, "http", "OnlineTrackLookup").Validate(testkit.DecodeJSON(t, body)) == nil {
		t.Fatal("unavailable collection identity advertised playable metadata")
	}
}

func TestIncompleteBilibiliObservationKeepsUnknownCount(t *testing.T) {
	observation, err := bilibiliContractClient(t).ObservePlaylist(context.Background(), "favorite:99")
	if err != nil {
		t.Fatal(err)
	}
	observation.Complete = false
	observation.Playlist.TrackCount = nil
	observation.MetadataDigest = online.PlaylistDigest(observation.Playlist, observation.Entries)
	if err := online.ValidateObservation(online.Bilibili, observation, "favorite:99"); err != nil {
		t.Fatal(err)
	}
	bilibiliMetadataContract(t, "Observation", observation)
	body, _ := json.Marshal(observation)
	value := testkit.DecodeJSON(t, body).(map[string]any)
	delete(value["playlist"].(map[string]any), "trackCount")
	if err := onlineSchema(t, "Observation").Validate(value); err != nil {
		t.Fatal(err)
	}
	if err := testkit.ContractSchema(t, "http", "OnlineObservation").Validate(value); err != nil {
		t.Fatal(err)
	}
	value["complete"] = true
	if onlineSchema(t, "Observation").Validate(value) == nil || testkit.ContractSchema(t, "http", "OnlineObservation").Validate(value) == nil {
		t.Fatal("complete observation accepted an unknown total count")
	}
}

package storage_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func publishMultiArtistTracks(t *testing.T, store *sqlite.Store, owner, provider string, artists ...[]string) []library.Track {
	t.Helper()
	ctx := t.Context()
	if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan("music"), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]library.Observation, len(artists))
	for i, names := range artists {
		name := string(rune('A'+i)) + ".flac"
		items[i] = library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: "music",
			RelativePath: name, ObservedVersion: "v1", DisplayName: name, SizeBytes: 100}, Title: &name, Artists: names}
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, len(items), nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, owner, provider, "", 100)
	if err != nil || len(page.Items) != len(items) {
		t.Fatal(page, err)
	}
	return page.Items
}

func multiArtistMembers(t *testing.T, service *catalog.Service, query catalog.Query, expected ...string) {
	t.Helper()
	page, err := service.Search(t.Context(), "admin", query, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(page.Items))
	for i, raw := range page.Items {
		var track catalog.Track
		if err = json.Unmarshal(raw, &track); err != nil {
			t.Fatal(err)
		}
		ids[i] = track.ID
	}
	slices.Sort(ids)
	expected = slices.Clone(expected)
	slices.Sort(expected)
	if page.Total != len(expected) || !slices.Equal(ids, expected) {
		t.Fatalf("unexpected artist members: query=%+v total=%d ids=%v want=%v", query, page.Total, ids, expected)
	}
}

func TestMultiArtistPublicationCategoriesAndSourceIdentity(t *testing.T) {
	store, service, _ := catalogFixture(t)
	const provider = "multi-artists"
	const punctuation = "Band;Guest & Duo、Pair／Trio"
	tracks := publishMultiArtistTracks(t, store, "admin", provider,
		[]string{" \tSinger A / 歌手乙// ", "歌手乙 / Singer A / ", "Singer A", " \u3000", " " + punctuation + "\u3000", "singer a"},
		[]string{"歌手乙 / Third", "Singer A/Singer A"},
		[]string{"/ //", " \t ", "\u3000/\n"},
		[]string{"未知歌手"})
	wantArtists := [][]string{{"Singer A", "歌手乙", punctuation, "singer a"}, {"歌手乙", "Third", "Singer A"}, {}, {"未知歌手"}}
	for i, track := range tracks {
		if !slices.Equal(track.Artists, wantArtists[i]) {
			t.Fatalf("unexpected published artists: track=%s artists=%q want=%q", track.DisplayName, track.Artists, wantArtists[i])
		}
		stored, err := store.FindTrack(t.Context(), "admin", track.ID)
		if err != nil || !slices.Equal(stored.Artists, wantArtists[i]) {
			t.Fatalf("unexpected stored artists: track=%s artists=%q err=%v", track.ID, stored.Artists, err)
		}
	}
	other := publishMultiArtistTracks(t, store, "admin", "multi-artists-other", []string{" Singer A / Singer A "})
	publishMultiArtistTracks(t, store, "bob", provider, []string{"Singer A / 歌手乙"})

	categories := categorySearch(t, service, catalog.Query{CategoryKind: "artist", ProviderInstanceID: provider})
	if len(categories) != 7 {
		t.Fatalf("unexpected artist categories: %+v", categories)
	}
	wantMembers := map[string][]string{
		"Singer A": {tracks[0].ID, tracks[1].ID}, "歌手乙": {tracks[0].ID, tracks[1].ID},
		punctuation: {tracks[0].ID}, "singer a": {tracks[0].ID}, "Third": {tracks[1].ID}, "未知歌手": {tracks[3].ID},
	}
	for name, ids := range wantMembers {
		category := categoryFind(t, categories, provider, name, false)
		if category.TrackCount != len(ids) {
			t.Fatalf("unexpected category count: %+v want=%d", category, len(ids))
		}
		multiArtistMembers(t, service, catalog.Query{CategoryKind: "artist", CategoryID: category.ID}, ids...)
	}
	unknown := categoryFind(t, categories, provider, "", true)
	if unknown.TrackCount != 1 || unknown.ID == categoryFind(t, categories, provider, "未知歌手", false).ID {
		t.Fatalf("unknown and literal artist categories collided: %+v", categories)
	}
	multiArtistMembers(t, service, catalog.Query{CategoryKind: "artist", CategoryID: unknown.ID}, tracks[2].ID)
	multiArtistMembers(t, service, catalog.Query{ProviderInstanceID: provider, UnknownFields: []string{"artist"}}, tracks[2].ID)
	facets, err := service.Facets(t.Context(), "admin", provider)
	if err != nil || facets.UnknownCounts["artist"] != 1 {
		t.Fatal(facets, err)
	}
	mainCategory := categoryFind(t, categories, provider, "Singer A", false)
	otherCategory := categoryFind(t, categorySearch(t, service, catalog.Query{CategoryKind: "artist", ProviderInstanceID: "multi-artists-other"}), "multi-artists-other", "Singer A", false)
	if otherCategory.ID == mainCategory.ID || otherCategory.TrackCount != 1 {
		t.Fatalf("artist categories crossed provider boundaries: %+v %+v", mainCategory, otherCategory)
	}
	multiArtistMembers(t, service, catalog.Query{CategoryKind: "artist", CategoryID: otherCategory.ID}, other[0].ID)
	_, err = service.Search(t.Context(), "admin", catalog.Query{CategoryKind: "artist", CategoryID: mainCategory.ID, ProviderInstanceID: "multi-artists-other"}, "", 200)
	matchingError(t, err, "NOT_FOUND")
	_, err = service.Search(t.Context(), "bob", catalog.Query{CategoryKind: "artist", CategoryID: mainCategory.ID}, "", 200)
	matchingError(t, err, "NOT_FOUND")

	page, err := service.Search(t.Context(), "admin", catalog.Query{Type: "artists", ProviderInstanceID: provider}, "", 200)
	if err != nil || page.Total != len(wantMembers) || len(page.Items) != len(wantMembers) {
		t.Fatal(page, err)
	}
	seen := map[string]bool{}
	for _, raw := range page.Items {
		var artist catalog.Artist
		if err = json.Unmarshal(raw, &artist); err != nil {
			t.Fatal(err)
		}
		ids, exists := wantMembers[artist.Name]
		if !exists || seen[artist.Name] || artist.ProviderInstanceID != provider || artist.TrackCount != len(ids) || artist.IdentityKind != "localCredit" {
			t.Fatalf("unexpected artist entity: %+v", artist)
		}
		seen[artist.Name] = true
		multiArtistMembers(t, service, catalog.Query{ArtistID: artist.ID}, ids...)
		multiArtistMembers(t, service, catalog.Query{ArtistID: artist.ID, ProviderInstanceID: "multi-artists-other"})
		detail, err := service.Artist(t.Context(), "admin", artist.ID)
		if err != nil || detail.Name != artist.Name || detail.TrackCount != len(ids) {
			t.Fatal(detail, err)
		}
		_, err = service.Artist(t.Context(), "bob", artist.ID)
		matchingError(t, err, "NOT_FOUND")
	}
}

func TestMultiArtistOnlinePublicationAndUpdate(t *testing.T) {
	store, _, _ := catalogFixture(t)
	metadata := online.Track{ExternalID: "collaboration", Title: "Online collaboration", Artists: []string{" 歌手甲 / 歌手乙 ", "歌手乙//Singer C", "歌手甲"}}
	track, err := store.PublishOnlineTrack(t.Context(), "admin", "online-multi-artists", metadata)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"歌手甲", "歌手乙", "Singer C"}
	if !slices.Equal(track.Artists, want) {
		t.Fatalf("unexpected online artists: %q want=%q", track.Artists, want)
	}
	stored, err := store.FindTrack(t.Context(), "admin", track.ID)
	if err != nil || !slices.Equal(stored.Artists, want) {
		t.Fatal(stored, err)
	}
	metadata.Artists = []string{" / / ", "\t"}
	updated, err := store.PublishOnlineTrack(t.Context(), "admin", "online-multi-artists", metadata)
	if err != nil || updated.ID != track.ID || len(updated.Artists) != 0 {
		t.Fatal(updated, err)
	}
	stored, err = store.FindTrack(t.Context(), "admin", track.ID)
	if err != nil || len(stored.Artists) != 0 {
		t.Fatal(stored, err)
	}
}

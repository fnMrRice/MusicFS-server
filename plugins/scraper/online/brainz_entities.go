package online

import (
	"errors"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type brainzCredit struct {
	Name       string
	Artist     struct{ Name string }
	JoinPhrase string `json:"joinphrase"`
}

type brainzAlias struct {
	Name, Locale, Type string
}

func brainzNames(primary string, aliases []brainzAlias) []scraper.LocalizedTitle {
	var names []scraper.LocalizedTitle
	for _, alias := range aliases {
		if !strings.EqualFold(alias.Type, "Search hint") {
			names = addPlatformName(names, primary, alias.Locale, alias.Name)
		}
	}
	return names
}

type brainzEntity struct {
	ID, Name, Title, Disambiguation, Country, Date, Type, Status string
	Length                                                       int64
	Artists                                                      []brainzCredit              `json:"artist-credit"`
	LifeSpan                                                     struct{ Begin, End string } `json:"life-span"`
	Area                                                         struct{ Name string }
	ISRCs                                                        []string
	Aliases                                                      []brainzAlias
	Releases                                                     []struct{ Title string }
	Labels                                                       []struct {
		Catalog string `json:"catalog-number"`
		Label   struct{ Name string }
	} `json:"label-info"`
	Media []struct {
		Position   int
		TrackCount int `json:"track-count"`
		Tracks     []struct {
			ID        string
			Position  int
			Title     string
			Length    int64
			Artists   []brainzCredit `json:"artist-credit"`
			Recording struct {
				Title   string
				Aliases []brainzAlias
				Length  int64
				Artists []brainzCredit `json:"artist-credit"`
			}
		}
	}
}

// 上游声明的曲数必须完整返回，避免把截断曲目表当作整张专辑。
func (row brainzEntity) validateTrackList() error {
	for _, medium := range row.Media {
		if medium.TrackCount > 0 && medium.TrackCount != len(medium.Tracks) {
			return errors.New("incomplete MusicBrainz album track list")
		}
		for _, track := range medium.Tracks {
			if strings.TrimSpace(track.Title) == "" && strings.TrimSpace(track.Recording.Title) == "" {
				return errors.New("MusicBrainz album track title is missing")
			}
		}
	}
	return nil
}

func brainzCredits(credits []brainzCredit) string {
	var b strings.Builder
	for _, credit := range credits {
		name := credit.Name
		if name == "" {
			name = credit.Artist.Name
		}
		b.WriteString(name)
		b.WriteString(credit.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

func (row brainzEntity) item(entity string) scraper.Item {
	item := scraper.Item{ID: row.ID, Title: row.Title, Artist: brainzCredits(row.Artists), Date: row.Date,
		SourceURL: brainzSite + "/" + entity + "/" + row.ID}
	details := []string{row.Disambiguation}
	switch entity {
	case "release":
		item.Album = row.Title
		details = append(details, row.Status, row.Country)
		publishers := []string{}
		for _, label := range row.Labels {
			if label.Label.Name != "" {
				publishers = append(publishers, label.Label.Name)
			}
			details = append(details, label.Catalog)
		}
		item.Publisher = strings.Join(publishers, " / ")
	case "artist":
		item.Title, item.Artist, item.Date = row.Name, row.Name, row.LifeSpan.Begin
		item.LocalizedArtistNames = brainzNames(row.Name, row.Aliases)
		details = append(details, row.Type, row.Area.Name, row.Country)
	case "recording":
		item.LocalizedTitles = brainzNames(row.Title, row.Aliases)
		item.Duration = float64(row.Length) / 1000
		// 同一录音可以跨多张专辑，仅在归属唯一时填入专辑。
		if len(row.Releases) == 1 {
			item.Album = row.Releases[0].Title
		}
		details = append(details, row.ISRCs...)
	}
	parts := details[:0]
	for _, detail := range details {
		if detail != "" {
			parts = append(parts, detail)
		}
	}
	item.Description = strings.Join(parts, " · ")
	return item
}

func (row brainzEntity) trackList() []scraper.Track {
	var tracks []scraper.Track
	for _, medium := range row.Media {
		for _, t := range medium.Tracks {
			title, length, artists := t.Title, t.Length, t.Artists
			if title == "" {
				title = t.Recording.Title
			}
			if length == 0 {
				length = t.Recording.Length
			}
			if len(artists) == 0 {
				artists = t.Recording.Artists
			}
			if title != "" {
				names := brainzNames(title, t.Recording.Aliases)
				names = addPlatformName(names, title, "und", t.Recording.Title)
				var externalID string
				if brainzID.MatchString(t.ID) {
					externalID = t.ID
				}
				tracks = append(tracks, scraper.Track{ExternalID: externalID, Title: title, LocalizedTitles: names, Artist: brainzCredits(artists),
					Number: t.Position, Disc: medium.Position, Duration: float64(length) / 1000})
			}
		}
	}
	return tracks
}

package online

import (
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type brainzCredit struct {
	Name       string
	Artist     struct{ Name string }
	JoinPhrase string `json:"joinphrase"`
}

type brainzEntity struct {
	ID, Name, Title, Disambiguation, Country, Date, Type, Status string
	Length                                                       int64
	Artists                                                      []brainzCredit              `json:"artist-credit"`
	LifeSpan                                                     struct{ Begin, End string } `json:"life-span"`
	Area                                                         struct{ Name string }
	ISRCs                                                        []string
	Releases                                                     []struct{ Title string }
	Labels                                                       []struct {
		Catalog string `json:"catalog-number"`
		Label   struct{ Name string }
	} `json:"label-info"`
	Media []struct {
		Position int
		Tracks   []struct {
			Position  int
			Title     string
			Length    int64
			Artists   []brainzCredit `json:"artist-credit"`
			Recording struct {
				Title   string
				Length  int64
				Artists []brainzCredit `json:"artist-credit"`
			}
		}
	}
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
		for _, label := range row.Labels {
			details = append(details, label.Label.Name, label.Catalog)
		}
	case "artist":
		item.Title, item.Artist, item.Date = row.Name, row.Name, row.LifeSpan.Begin
		details = append(details, row.Type, row.Area.Name, row.Country)
	case "recording":
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
				tracks = append(tracks, scraper.Track{Title: title, Artist: brainzCredits(artists),
					Number: t.Position, Disc: medium.Position, Duration: float64(length) / 1000})
			}
		}
	}
	return tracks
}

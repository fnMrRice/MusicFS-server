package online

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// DLsite 按 RJ 作品编号查询商店页面自身使用的资料接口。
func DLsite(client *http.Client) scraper.Plugin {
	plugin := newLookupPlugin(client, "dlsite", "DLsite", []string{"workDetails", "artwork"}, searchDLsite, "img.dlsite.jp", "img.dlsite.com")
	plugin.descriptor.ExternalIDNamespace = "dlsite"
	plugin.descriptor.RequiresExternalID = true
	return plugin
}

var dlsiteNumber = regexp.MustCompile(`^RJ[0-9]{6,8}$`)

func searchDLsite(ctx context.Context, client *http.Client, field string, q scraper.Query) ([]scraper.Item, error) {
	id := strings.ToUpper(strings.TrimSpace(q.ExternalID))
	if !dlsiteNumber.MatchString(id) {
		return nil, fault.Fail("INVALID_ARGUMENT", "DLsite RJ work ID required")
	}
	var works map[string]struct {
		Title  string `json:"work_name"`
		Circle string `json:"maker_name"`
		Date   string `json:"regist_date"`
		Image  string `json:"work_image"`
		Site   string `json:"site_id"`
	}
	params := url.Values{"product_id": {id}}
	if err := lookupJSON(ctx, client, "https://www.dlsite.com/home/product/info/ajax", params, nil, &works); err != nil {
		return nil, err
	}
	row, ok := works[id]
	if !ok || row.Title == "" {
		return []scraper.Item{}, nil
	}
	site := row.Site
	switch site {
	case "home", "maniax", "girls", "bl":
	default:
		site = "home"
	}
	item := scraper.Item{ID: id, Title: row.Title, Artist: row.Circle, Date: row.Date, SourceURL: "https://www.dlsite.com/" + site + "/work/=/product_id/" + id + ".html"}
	if field == "artwork" {
		item.ImageURL = lookupImage(row.Image, "img.dlsite.jp", "img.dlsite.com")
		if item.ImageURL == "" {
			return []scraper.Item{}, nil
		}
	}
	return []scraper.Item{item}, nil
}

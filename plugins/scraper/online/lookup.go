package online

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type lookupPlugin struct {
	descriptor scraper.Descriptor
	client     *http.Client
	search     func(context.Context, *http.Client, string, scraper.Query) ([]scraper.Item, error)
}

func newLookupPlugin(client *http.Client, id, name string, fields []string, search func(context.Context, *http.Client, string, scraper.Query) ([]scraper.Item, error), imageHosts ...string) *lookupPlugin {
	if client == nil {
		client = http.DefaultClient
	}
	return &lookupPlugin{scraper.Descriptor{PluginID: "musicfs.scraper." + id, Name: name, Type: "scraper", Fields: fields, ImageHosts: imageHosts, Available: true}, client, search}
}

func (p *lookupPlugin) Descriptor() scraper.Descriptor {
	d := p.descriptor
	d.Fields = slices.Clone(d.Fields)
	d.ImageHosts = slices.Clone(d.ImageHosts)
	return d
}

func (p *lookupPlugin) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if !slices.Contains(p.descriptor.Fields, field) {
		return nil, fault.Fail("UNSUPPORTED", "unsupported scraper field")
	}
	items, err := p.search(ctx, p.client, field, q)
	if errors.Is(err, errCatalogMissing) {
		return []scraper.Item{}, nil
	}
	return items, err
}

func lookupJSON(ctx context.Context, client *http.Client, endpoint string, params url.Values, payload any, result any) error {
	method := http.MethodGet
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		method = http.MethodPost
	}
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return readCatalogJSON(client, req, result)
}

func lookupNumericID(raw string) (string, error) {
	id, err := strconv.ParseUint(raw, 10, 63)
	if err != nil || id == 0 {
		return "", fault.Fail("INVALID_ARGUMENT", "invalid external ID")
	}
	return strconv.FormatUint(id, 10), nil
}

// 只接受资料站公布的 HTTPS 图片地址，不向结果传递任意外链。
func lookupImage(raw string, hosts ...string) string {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !slices.Contains(hosts, strings.ToLower(u.Host)) {
		return ""
	}
	return u.String()
}

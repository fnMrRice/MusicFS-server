package onlinescrape

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

// AlbumArtwork 持久保存每个用户的专辑封面。普通读取不按时间重新验证上游。
type AlbumArtwork struct {
	root      string
	catalog   *Service
	repo      music.Repository
	sourceURL func(context.Context, string, string, bool) (string, error)
	slots     [8]chan struct{}
}

func NewAlbumArtwork(root string, catalog *Service, repo music.Repository, sourceURL func(context.Context, string, string, bool) (string, error)) *AlbumArtwork {
	a := &AlbumArtwork{root: root, catalog: catalog, repo: repo, sourceURL: sourceURL}
	for i := range a.slots {
		a.slots[i] = make(chan struct{}, 1)
	}
	return a
}

func (a *AlbumArtwork) Load(ctx context.Context, owner, id string, refresh bool) ([]byte, string, error) {
	album, err := a.repo.MusicAlbum(ctx, owner, id)
	if err != nil {
		return nil, "", err
	}
	if album.CoverPath == nil {
		return nil, "", noAlbumArt()
	}
	ref := *album.CoverPath
	source, ok := strings.CutPrefix(ref, "/api/v1/source-tracks/")
	if ok {
		source, ok = strings.CutSuffix(source, "/online-artwork")
		if !ok || !works.ValidID(source) {
			return nil, "", noAlbumArt()
		}
	} else {
		source = ""
		if !strings.HasPrefix(ref, "https://") {
			return nil, "", noAlbumArt()
		}
	}

	// 成员曲目撤销共享或变更时，不复用旧成员的封面。刮削图片地址变化由主动更新处理。
	key := sha256.Sum256([]byte(owner + "\x00" + id + "\x00" + source))
	slot := a.slots[int(key[0])%len(a.slots)]
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	name := filepath.Join(a.root, hex.EncodeToString(key[:])+".cover")
	if !refresh {
		data, err := os.ReadFile(name)
		if err == nil {
			return data, http.DetectContentType(data), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
	}

	fetch, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	field, err := a.field(fetch, owner, album, source, refresh)
	if err != nil {
		return nil, "", err
	}
	body, _, err := a.catalog.OpenMedia(fetch, owner, field, nil)
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, content.MaxArtworkBytes+1))
	if err != nil {
		return nil, "", err
	}
	image, err := content.Validate("artwork", data)
	if err != nil {
		return nil, "", domain.Fail("UNSUPPORTED", "Invalid album artwork")
	}
	if err = fetch.Err(); err != nil {
		return nil, "", err
	}
	if err = a.save(name, image.Bytes); err != nil {
		return nil, "", err
	}
	return image.Bytes, image.MIME, nil
}

func noAlbumArt() error { return domain.Fail("NOT_FOUND", "No remote album artwork is available") }

func (a *AlbumArtwork) field(ctx context.Context, owner string, album music.Album, source string, refresh bool) (works.ScrapedField, error) {
	var field works.ScrapedField
	field.Field = "artwork"
	if source != "" {
		if a.sourceURL == nil {
			return field, noAlbumArt()
		}
		raw, err := a.sourceURL(ctx, owner, source, refresh)
		field.PluginID = "musicfs.scraper.netease"
		field.Item.ImageURL = raw
		return field, err
	}
	fields, err := a.repo.MusicAlbumScrapedFields(ctx, owner, album.ID)
	if err != nil {
		return field, err
	}
	field.Item.ImageURL = *album.CoverPath
	for _, key := range []string{"artwork", "albumDetails", "albumTracks"} {
		for _, candidate := range fields {
			if candidate.Field != key || candidate.Item.ImageURL != field.Item.ImageURL {
				continue
			}
			// 分字段合并的详情可能保留另一插件的图片；由声明该主机的来源处理下载与更新。
			plugin := a.catalog.plugins[candidate.PluginID]
			if plugin == nil || !scraper.AllowsImageURL(field.Item.ImageURL, plugin.Descriptor().ImageHosts) {
				continue
			}
			field.PluginID = candidate.PluginID
			if refresh && candidate.Item.ID != "" {
				field.Item.ImageURL, err = a.latestURL(ctx, owner, candidate)
			}
			return field, err
		}
	}
	// 手工指定的封面也只交给明确声明了该图片主机的插件。
	for _, plugin := range a.catalog.List() {
		if plugin.Available && scraper.AllowsImageURL(field.Item.ImageURL, plugin.ImageHosts) {
			field.PluginID = plugin.PluginID
			return field, nil
		}
	}
	return field, noAlbumArt()
}

// 显式更新时按已选发行版重新解析图片，不把搜索结果中的其他版本当作本专辑。
func (a *AlbumArtwork) latestURL(ctx context.Context, owner string, selected works.ScrapedField) (string, error) {
	plugin := a.catalog.plugins[selected.PluginID]
	if plugin == nil {
		return "", noAlbumArt()
	}
	field := selected.Field
	if !slices.Contains(plugin.Descriptor().Fields, field) {
		field = "albumDetails"
	}
	item := selected.Item
	query := scraper.Query{ExternalID: item.ID, SourceURL: item.SourceURL, Title: item.Title, Artist: item.Artist, Album: item.Album}
	page, err := a.catalog.lookup(ctx, owner, selected.PluginID, field, query, field == "albumDetails" || field == "albumTracks")
	if err != nil {
		return "", err
	}
	for _, entry := range page.items {
		if entry.ID == item.ID && entry.ImageURL != "" {
			return entry.ImageURL, nil
		}
	}
	return "", noAlbumArt()
}

func (a *AlbumArtwork) save(name string, data []byte) error {
	if err := os.MkdirAll(a.root, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(a.root, ".album-cover-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// 先校验、写完并关闭新文件。下载或写入失败时保留上次可用的封面。
	return os.Rename(tmp.Name(), name)
}

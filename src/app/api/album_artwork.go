package api

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

type albumArtwork interface {
	Load(context.Context, string, string, bool) ([]byte, string, error)
}

func (s *Server) RegisterAlbumArtwork(service albumArtwork) {
	s.HandleMediaDelivery("GET /api/v1/library/albums/{id}/artwork", func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		data, mime, err := service.Load(r.Context(), a.User.ID, r.PathValue("id"), false)
		if err != nil {
			return err
		}
		if mime != "image/jpeg" && mime != "image/png" {
			return domain.Fail("UNSUPPORTED", "Unsupported album artwork format")
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, err = w.Write(data)
		return err
	})
	s.register("POST /api/v1/library/albums/{id}/artwork/refresh", true, true, MediaDeliveryTimeout, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		if r.ContentLength != 0 {
			if err := DecodeJSON(w, r, &struct{}{}); err != nil {
				return err
			}
		}
		if _, _, err := service.Load(r.Context(), a.User.ID, r.PathValue("id"), true); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

func albumArtworkPath(id string) string {
	return "/api/v1/library/albums/" + url.PathEscape(id) + "/artwork"
}

func publicAlbum(album music.Album) music.Album {
	if album.ID == "" || album.CoverPath == nil {
		return album
	}
	raw := *album.CoverPath
	id, isSource := strings.CutPrefix(raw, "/api/v1/source-tracks/")
	id, isArtwork := strings.CutSuffix(id, "/online-artwork")
	if strings.HasPrefix(raw, "https://") || (isSource && isArtwork && personal.ValidID(id)) {
		path := albumArtworkPath(album.ID)
		album.CoverPath = &path
	}
	return album
}

func publicTrack(track music.Track) music.Track {
	track.Albums = slices.Clone(track.Albums)
	for i := range track.Albums {
		track.Albums[i] = publicAlbum(track.Albums[i])
	}
	return track
}

// 只转换用户可见的曲库资料，保留数据库与插件之间用于下载的原始引用。
func publicMusicValue[T any](value T) T {
	switch item := any(&value).(type) {
	case *music.Album:
		*item = publicAlbum(*item)
	case *music.Track:
		*item = publicTrack(*item)
	case *music.AlbumPage:
		item.Items = slices.Clone(item.Items)
		for i := range item.Items {
			item.Items[i].Album = publicAlbum(item.Items[i].Album)
		}
	case *music.TrackPage:
		item.Items = slices.Clone(item.Items)
		for i := range item.Items {
			item.Items[i] = publicTrack(item.Items[i])
		}
	}
	return value
}

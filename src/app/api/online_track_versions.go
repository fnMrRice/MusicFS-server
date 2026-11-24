package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

type pageDownloads struct {
	DownloadVersionsByTrack map[string][]onlinecache.Version `json:"downloadVersionsByTrack,omitempty"`
}

// 仅在 HTTP 响应中补上本地下载。插件结果和跨请求复用的歌单快照都只包含在线资料。
func (s *Server) pageDownloads(r *http.Request, owner string, ids []string) (pageDownloads, error) {
	if s.options.OnlineDownloads == nil || len(ids) == 0 {
		return pageDownloads{}, nil
	}
	versions, err := s.options.OnlineDownloads.OnlineTrackVersions(r.Context(), owner, r.PathValue("id"), ids)
	return pageDownloads{DownloadVersionsByTrack: versions}, err
}

func externalTrackIDs(tracks []online.Track) []string {
	ids := make([]string, len(tracks))
	for i, track := range tracks {
		ids[i] = track.ExternalID
	}
	return ids
}

func (s *Server) writeOnlineSearchPage(w http.ResponseWriter, r *http.Request, owner string, page online.SearchPage) error {
	downloads, err := s.pageDownloads(r, owner, externalTrackIDs(page.Tracks))
	if err != nil {
		return err
	}
	return WriteJSON(w, http.StatusOK, struct {
		online.SearchPage
		pageDownloads
	}{page, downloads})
}

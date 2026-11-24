// Package onlineartwork 将曲目来源的官方封面按需保存为本地媒体对象。
package onlineartwork

import (
	"context"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type Tracks interface {
	Track(context.Context, string, string) (library.Track, error)
}

type Metadata interface {
	Runtime(context.Context, string, string, string) (onlinehost.Runtime, error)
	MetadataBatch(context.Context, string, string, []string) (online.TrackBatch, error)
}

type Service struct {
	tracks   Tracks
	online   Metadata
	media    *mediastore.ContentService
	client   http.Client
	batch    coverBatches
	mu       sync.Mutex
	requests map[[2]string]*pendingArtwork
	changed  chan struct{}
	gate     chan struct{}
}

func New(tracks Tracks, provider Metadata, media *mediastore.ContentService, transport http.RoundTripper) *Service {
	s := &Service{tracks: tracks, online: provider, media: media, gate: make(chan struct{}, 6), requests: map[[2]string]*pendingArtwork{}, changed: make(chan struct{})}
	s.client = http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 || library.NetEaseArtworkURL(req.URL.String()) == "" {
			return domain.Fail("UNSUPPORTED", "Invalid artwork redirect")
		}
		return nil
	}}
	s.batch = coverBatches{provider: provider, pending: make(map[scope][]*coverQuery), slots: make(chan struct{}, 2)}
	return s
}

// Open 优先交付当前用户曲目已选定的本地媒体，远端实例只参与缺失封面的获取。
func (s *Service) Open(ctx context.Context, owner, id string) (mediastore.ContentObject, io.ReadCloser, error) {
	var empty mediastore.ContentObject
	t, err := s.tracks.Track(ctx, owner, id)
	if err != nil {
		return empty, nil, err
	}
	if t.RootID != "online-audio" || t.Path != "" {
		return empty, nil, missing()
	}
	if t.CoverObjectID != nil {
		return s.media.Open(ctx, owner, *t.CoverObjectID)
	}
	if t.Availability != "available" {
		return empty, nil, missing()
	}
	object, err := s.waitArtwork(ctx, owner, t)
	if err != nil {
		return empty, nil, err
	}
	return s.media.Open(ctx, owner, object)
}

func missing() error { return domain.Fail("NOT_FOUND", "No online artwork is available") }

func (s *Service) objectID(ctx context.Context, owner string, t library.Track) (string, error) {
	id := t.ID
	r, err := s.online.Runtime(ctx, owner, t.ProviderID, "")
	if err != nil {
		return "", err
	}
	if r.Platform != online.Netease {
		return "", missing()
	}
	raw := t.ProviderCoverURL
	if raw == "" {
		raw, err = s.batch.lookup(ctx, scope{owner, t.ProviderID}, t.ExternalID)
		if err != nil {
			return "", err
		}
	}
	raw = library.NetEaseArtworkURL(raw)
	if raw == "" {
		return "", missing()
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	selection, err := s.media.ContentSelection(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if selection.Target.SourceTrackID != t.ID || selection.Target.AssetID != t.AssetID || selection.Target.AssetVersion != t.Version {
		return "", domain.Fail("RESOURCE_CHANGED", "Artwork target changed")
	}
	for _, binding := range selection.Bindings {
		if binding.Field == "artwork" && binding.Current && binding.State == "available" {
			return binding.ObjectID, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", missing()
	}
	req.Header.Set("Accept", "image/jpeg, image/png")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", domain.Fail("SOURCE_UNAVAILABLE", "Artwork request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", missing()
	}
	if resp.StatusCode != http.StatusOK {
		return "", domain.Fail("SOURCE_UNAVAILABLE", "Artwork is unavailable")
	}
	meta := mediastore.ContentMetadata{Target: selection.Target, Field: "artwork", Origin: mediastore.Origin{Kind: "import", InstanceID: t.ProviderID, CandidateID: t.ExternalID}}
	declared := resp.Header.Get("Content-Type")
	// 网易云 CDN 对部分 JPEG 声明 image/jpg，媒体库仍会检测文件头并完整解码图片。
	if mediaType, _, parseErr := mime.ParseMediaType(declared); parseErr == nil && mediaType == "image/jpg" {
		declared = "image/jpeg"
	}
	object, err := s.media.UploadBackground(ctx, owner, meta, declared, domain.NewID(), resp.Body)
	if err != nil {
		return "", err
	}
	_, chooseErr := s.media.SelectContent(ctx, owner, mediastore.SelectRequest{Target: selection.Target, ExpectedRevision: selection.Revision, ObjectID: object.ID, Automatic: true}, domain.NewID())
	// 录音封面属于用户选择，下载期间另一个客户端可能已经完成了选择。
	current, err := s.tracks.Track(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if current.CoverObjectID != nil {
		return *current.CoverObjectID, nil
	}
	if chooseErr != nil {
		return "", chooseErr
	}
	return "", missing()
}

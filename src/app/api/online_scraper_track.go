package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type onlineTrackMedia interface {
	mediaAPI
	CheckOnlineSelection(context.Context, string, mediastore.SelectRequest, string) (mediastore.Selection, bool, error)
}

func (s *Server) registerScrapedTrack(catalog *onlinescrape.Service, media onlineTrackMedia, client *http.Client) {
	s.Handle("PUT /api/v1/source-tracks/{id}/scraped-fields/{field}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			scrapedFieldInput
			AssetID      string `json:"assetId"`
			AssetVersion string `json:"assetVersion"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.AssetID == "" || input.AssetVersion == "" || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
			return domain.Fail("INVALID_ARGUMENT", "Asset version and media revision are required")
		}
		key := r.Header.Get("Idempotency-Key")
		if len(key) < 8 || !works.ValidID(key) {
			return domain.Fail("INVALID_ARGUMENT", "Invalid idempotency key")
		}
		ctx, owner := r.Context(), session.User.ID
		target := mediastore.ContentTarget{SourceTrackID: r.PathValue("id"), AssetID: input.AssetID, AssetVersion: input.AssetVersion}
		request := mediastore.SelectRequest{Target: target, ExpectedRevision: *input.ExpectedRevision, Online: &mediastore.OnlineCandidate{PluginID: input.PluginID, Field: r.PathValue("field"), Token: input.CandidateToken}}
		replayed, ok, err := media.CheckOnlineSelection(ctx, owner, request, key)
		if err != nil {
			return err
		}
		if ok {
			return WriteJSON(w, http.StatusOK, replayed)
		}
		selected, err := catalog.Candidate(owner, input.PluginID, r.PathValue("field"), input.CandidateToken)
		if err != nil {
			return err
		}
		body, mime, err := catalog.OpenMedia(ctx, selected, client)
		if err != nil {
			return err
		}
		defer body.Close()
		// 选用的内部上传拥有独立键空间，避免与用户手动上传的相同键碰撞。
		digest := sha256.Sum256([]byte(key))
		uploadKey := "online-scraper-" + hex.EncodeToString(digest[:])
		object, err := media.Upload(ctx, owner, mediastore.ContentMetadata{Target: target, Field: selected.Field, Origin: mediastore.Origin{Kind: "scraper", InstanceID: input.PluginID, CandidateID: selected.Item.ID}}, mime, uploadKey, body)
		if err != nil {
			return err
		}
		request.ObjectID = object.ID
		selection, err := media.SelectContent(ctx, owner, request, key)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, selection)
	})
}

package api

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

type mediaAPI interface {
	Upload(context.Context, string, mediastore.ContentMetadata, string, string, io.Reader) (mediastore.ContentObject, error)
	Open(context.Context, string, string) (mediastore.ContentObject, io.ReadCloser, error)
	ContentObject(context.Context, string, string) (mediastore.ContentObject, error)
	ContentSelection(context.Context, string, string) (mediastore.Selection, error)
	SelectContent(context.Context, string, mediastore.SelectRequest, string) (mediastore.Selection, error)
	UnlockContent(context.Context, string, mediastore.UnlockRequest, string) (mediastore.Selection, error)
}

func (s *Server) RegisterMedia(service mediaAPI) {
	derived := derivedMedia(service)
	s.registerDerivedMedia(derived)
	s.HandleMediaUpload("POST /api/v1/media", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return domain.Fail("INVALID_ARGUMENT", "Invalid media parameters")
		}
		metadata, err := mediaUploadMetadata(query)
		if err != nil {
			return err
		}
		declared := r.Header.Get("Content-Type")
		if declared == "" {
			return domain.Fail("INVALID_ARGUMENT", "An actual media Content-Type is required")
		}
		if _, _, err := mime.ParseMediaType(declared); err != nil {
			return domain.Fail("INVALID_ARGUMENT", "Invalid media Content-Type")
		}
		object, err := service.Upload(r.Context(), session.User.ID, metadata, declared, r.Header.Get("Idempotency-Key"), r.Body)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, object)
	})
	s.Handle("GET /api/v1/media/{id}/info", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		object, err := service.ContentObject(r.Context(), session.User.ID, r.PathValue("id"))
		if mediaNotFound(err) && derived != nil {
			o, e := derived.DerivedObject(r.Context(), session.User.ID, r.PathValue("id"))
			if e != nil {
				return e
			}
			return WriteJSON(w, http.StatusOK, o)
		}
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, object)
	})
	s.HandleMediaDelivery("GET /api/v1/media/{id}", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		object, body, err := service.Open(r.Context(), session.User.ID, r.PathValue("id"))
		if mediaNotFound(err) && derived != nil {
			return deliverDerived(w, r, session, derived)
		}
		if err != nil {
			return err
		}
		defer body.Close()
		extension := map[string]string{"text": "txt", "lrc": "lrc", "png": "png", "jpeg": "jpg"}[object.Metadata.Format]
		if extension == "" {
			return domain.Fail("UNSUPPORTED", "Unsupported media format")
		}
		w.Header().Set("Content-Type", object.MIME)
		w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
		w.Header().Set("Content-Disposition", `inline; filename="media.`+extension+`"`)
		w.Header().Set("Accept-Ranges", "none")
		w.WriteHeader(http.StatusOK)
		_, err = io.CopyN(w, body, object.Size)
		return err
	})
	s.Handle("GET /api/v1/source-tracks/{id}/media-selection", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		selection, err := service.ContentSelection(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, selection)
	})
	s.Handle("PUT /api/v1/source-tracks/{id}/media-selection", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ObjectID         string `json:"objectId"`
			AssetID          string `json:"assetId"`
			AssetVersion     string `json:"assetVersion"`
			ExpectedRevision *int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.ObjectID == "" || input.AssetID == "" || input.AssetVersion == "" {
			return domain.Fail("INVALID_ARGUMENT", "Object, asset version and expectedRevision are required")
		}
		request := mediastore.SelectRequest{Target: mediastore.ContentTarget{SourceTrackID: r.PathValue("id"), AssetID: input.AssetID, AssetVersion: input.AssetVersion}, ExpectedRevision: *input.ExpectedRevision, ObjectID: input.ObjectID, Automatic: false}
		selection, err := service.SelectContent(r.Context(), session.User.ID, request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, selection)
	})
	s.Handle("POST /api/v1/source-tracks/{id}/media-selection/unlock", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Field            string `json:"field"`
			Language         string `json:"language"`
			ExpectedRevision *int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.Field == "" {
			return domain.Fail("INVALID_ARGUMENT", "Field and expectedRevision are required")
		}
		selection, err := service.UnlockContent(r.Context(), session.User.ID, mediastore.UnlockRequest{SourceTrackID: r.PathValue("id"), ExpectedRevision: *input.ExpectedRevision, Field: input.Field, Language: input.Language}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, selection)
	})
}

func mediaUploadMetadata(query url.Values) (mediastore.ContentMetadata, error) {
	allowed := map[string]bool{"sourceTrackId": true, "assetId": true, "assetVersion": true, "field": true, "language": true}
	for name, values := range query {
		if !allowed[name] || len(values) != 1 {
			return mediastore.ContentMetadata{}, domain.Fail("INVALID_ARGUMENT", "Unknown or repeated media parameter")
		}
	}
	for _, name := range []string{"sourceTrackId", "assetId", "assetVersion", "field"} {
		if query.Get(name) == "" {
			return mediastore.ContentMetadata{}, domain.Fail("INVALID_ARGUMENT", "Media target and field are required")
		}
	}
	return mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: query.Get("sourceTrackId"), AssetID: query.Get("assetId"), AssetVersion: query.Get("assetVersion")}, Field: query.Get("field"), Language: query.Get("language"), Origin: mediastore.Origin{Kind: "upload"}}, nil
}

package api

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

func deliverWorkAttachment(w http.ResponseWriter, r *http.Request, owner string, service *workpackages.Service) error {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) > 1 || (len(values) == 1 && (len(values["download"]) != 1 || values.Get("download") != "1")) {
		return invalidWorkInput()
	}
	download := values.Get("download") == "1"
	node, reader, close, err := service.OpenAttachment(r.Context(), owner, r.PathValue("id"), r.PathValue("packageId"), r.PathValue("nodeId"))
	if err != nil {
		return err
	}
	defer close()
	var content io.ReadSeeker = io.NewSectionReader(reader, 0, node.SizeBytes)
	mediaType := "application/octet-stream"
	disposition := "attachment"
	if !download {
		limit := int64(1 << 20)
		if node.AttachmentKind == "image" {
			limit = 16 << 20
		}
		if node.AttachmentKind == "download" || node.SizeBytes > limit {
			return domain.Fail("UNSUPPORTED", "Attachment is available for download only")
		}
		data, err := io.ReadAll(io.LimitReader(content, limit+1))
		if err != nil {
			return err
		}
		if int64(len(data)) != node.SizeBytes || reader.Validate() != nil {
			return domain.Fail("RESOURCE_CHANGED", "Attachment changed during read")
		}
		switch node.AttachmentKind {
		case "text":
			if !utf8.Valid(data) {
				return domain.Fail("UNSUPPORTED", "Text preview requires UTF-8; download the original file")
			}
			mediaType = "text/plain; charset=utf-8"
		case "image":
			mediaType = http.DetectContentType(data)
			if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" {
				return domain.Fail("UNSUPPORTED", "Image preview format is not supported")
			}
			config, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40_000_000 {
				return domain.Fail("UNSUPPORTED", "Image preview dimensions exceed the limit")
			}
		default:
			return domain.Fail("UNSUPPORTED", "Attachment preview is not supported")
		}
		content = bytes.NewReader(data)
		disposition = "inline"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": node.Name}))
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	http.ServeContent(w, r, node.Name, time.Time{}, content)
	return nil
}

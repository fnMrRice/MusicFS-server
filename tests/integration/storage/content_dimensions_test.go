package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func TestContentDimensionsRejectInvalidTrustedInput(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	input := contentInput(tracks[0], "", "validated-image")
	input.Metadata.Field, input.Metadata.Format, input.MIME = "artwork", "png", "image/png"
	for _, dimensions := range [][2]int{{-1, 1}, {0, 1}, {1, 0}, {8193, 1}, {1, 8193}, {8192, 8192}} {
		input.Width, input.Height = dimensions[0], dimensions[1]
		_, _, err := s.PrepareContent(t.Context(), "admin", input, domain.NewID())
		matchingError(t, err, "INVALID_ARGUMENT")
	}
	input.Width, input.Height = 17, 23
	object := contentPublish(t, s, "admin", input)
	if object.Width != 17 || object.Height != 23 {
		t.Fatal("validated dimensions did not survive publication", object)
	}
	input.Width, input.Height = 0, 0
	unknown := contentPublish(t, s, "admin", input)
	if unknown.Width != 0 || unknown.Height != 0 {
		t.Fatal("unknown dimensions were inferred", unknown)
	}
	lyrics := contentInput(tracks[0], "en", "Synthetic lyrics")
	lyrics.Width, lyrics.Height = 1, 1
	_, _, err := s.PrepareContent(t.Context(), "admin", lyrics, domain.NewID())
	matchingError(t, err, "INVALID_ARGUMENT")
}

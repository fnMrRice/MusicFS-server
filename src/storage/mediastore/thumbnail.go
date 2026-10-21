package mediastore

import (
	"bytes"
	"context"
	"image/png"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttransform"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

func (s *DerivedService) Thumbnail(ctx context.Context, owner, id string) (DerivedObject, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		return DerivedObject{}, domain.Fail("RATE_LIMITED", "Thumbnail generation is busy")
	}
	parent, in, err := s.content.Open(ctx, owner, id)
	if err != nil {
		return DerivedObject{}, err
	}
	defer in.Close()
	if parent.Metadata.Field != "artwork" {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Only artwork has a thumbnail")
	}
	if s.thumbnail == nil || s.thumbnail.Transform() != contenttransform.Thumbnail256V1 {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Thumbnail transform plugin is unavailable")
	}
	m := DerivedMetadata{Transform: s.thumbnail.Transform(), ParentObjectID: id, ParentSHA256: parent.SHA256}
	key, err := m.Key()
	if err != nil {
		return DerivedObject{}, err
	}
	if o, exists, e := s.existing(ctx, owner, key); e != nil || exists {
		return o, e
	}
	body, err := io.ReadAll(io.LimitReader(in, content.MaxArtworkBytes+1))
	if err != nil {
		return DerivedObject{}, err
	}
	if len(body) == 0 || len(body) > content.MaxArtworkBytes {
		return DerivedObject{}, domain.Fail("RESOURCE_CHANGED", "Invalid artwork size")
	}
	output, err := s.thumbnail.Thumbnail(ctx, body)
	if err != nil {
		return DerivedObject{}, err
	}
	// 插件只生成字节，宿主校验输出边界后继续已有派生对象发布流程。
	if len(output) == 0 || len(output) > content.MaxArtworkBytes {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Invalid thumbnail output size")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(output))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 256 || cfg.Height > 256 {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Invalid thumbnail output dimensions")
	}
	if _, err = png.Decode(bytes.NewReader(output)); err != nil {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Invalid thumbnail output")
	}
	return s.publish(ctx, owner, "thumbnail", "image/png", m, output)
}

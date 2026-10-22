package audioffmpeg

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ProbeOutputMetadata 读取原流属性，也为禁用声道插件时的转码选择提供输入声道。
// 复用已配置的探测器和插件租约，并限制并发及等待时间。
func (p *Processor) ProbeOutputMetadata(ctx context.Context, input io.ReadSeeker, size int64) (Metadata, error) {
	if p == nil || p.config.FFprobePath == "" {
		return Metadata{}, fmt.Errorf("UNSUPPORTED: playback metadata probe is unavailable")
	}
	lease, err := p.AcquireLease(ctx)
	if err != nil {
		return Metadata{}, err
	}
	defer lease.Close()
	if lease != nil {
		ctx = lease.Context()
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		return Metadata{}, fmt.Errorf("RATE_LIMITED: audio processing is busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return ProbeMetadata(ctx, p.config.FFprobePath, input, size)
}

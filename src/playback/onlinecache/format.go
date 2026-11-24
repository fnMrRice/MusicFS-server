package onlinecache

import (
	"context"
	"log/slog"
	"os"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
)

type Probe func(context.Context, *os.File, int64) (audiotypes.Metadata, error)

func (m *Manager) measure(ctx context.Context, file *os.File, v *Version) {
	if m.probe == nil {
		return
	}
	format, err := m.probe(ctx, file, v.SizeBytes)
	if err != nil || format.Codec == nil || *format.Codec == "" || format.Container == nil || *format.Container == "" ||
		format.MIME == "" || format.MIME == "application/octet-stream" || format.Channels < 1 || format.Channels > 32 ||
		format.SampleRateHz == nil || *format.SampleRateHz <= 0 || *format.SampleRateHz > 1048575 {
		return
	}
	v.MIME, v.Codec, v.Container = format.MIME, *format.Codec, *format.Container
	v.SampleRateHz, v.Channels = int(*format.SampleRateHz), format.Channels
	v.BitrateBps = nil
	if format.BitrateBps != nil && *format.BitrateBps > 0 {
		v.BitrateBps = format.BitrateBps
	}
	if format.BitDepth > 0 && format.BitDepth <= 64 {
		v.BitDepth = format.BitDepth
	}
	v.FormatMeasured = true
}

// 启动后在后台逐个探测原有文件。没有可用探测器或文件损坏时保留来源报告的资料，随后可再次探测。
func (m *Manager) measureExisting(ctx context.Context) {
	if m.probe == nil {
		return
	}
	m.mu.Lock()
	var ids []string
	for id, r := range m.records {
		if !r.FormatMeasured {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := m.measureRecord(ctx, id); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "Could not update downloaded audio metadata", "versionId", id, "error", err)
		}
	}
}

func (m *Manager) measureRecord(ctx context.Context, id string) error {
	m.mu.Lock()
	r, present := m.records[id]
	if !present || r.FormatMeasured {
		m.mu.Unlock()
		return nil
	}
	h, err := m.pin(r)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	defer h.Close()
	m.measure(ctx, h.file, &r.Version)
	if !r.FormatMeasured || ctx.Err() != nil {
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, present := m.records[id]
	if !present || current.FormatMeasured || !current.DownloadedAt.Equal(r.DownloadedAt) {
		return nil
	}
	if err := m.repo.UpdateOnlineDownload(ctx, r); err != nil {
		return err
	}
	m.records[id] = r
	return nil
}

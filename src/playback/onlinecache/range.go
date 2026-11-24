package onlinecache

import (
	"context"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// SeekableAudio 由会话持有一份。各读者使用自己的偏移和取消信号。
type SeekableAudio interface {
	io.ReadCloser
	ReadAvailableAt(context.Context, []byte, int64, int64) (int, error)
}

// CachedVersion 只返回与本次平台授权、账号及音质均相符的完整文件。
func (m *Manager) CachedVersion(scope Scope, a pluginproto.OnlineAudio) (Version, bool) {
	if a.Version == "" {
		return Version{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[m.byKey[Key(scope, a)]]
	if !ok || m.checkFile(r) != nil {
		return Version{}, false
	}
	return r.Version, true
}

func (m *Manager) OpenSeekable(ctx context.Context, scope Scope, a pluginproto.OnlineAudio, prefs Preferences, size int64, open OpenUpstream) (SeekableAudio, error) {
	if size <= 0 || size > pluginproto.MaxOnlineAudioBytes {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid online audio length")
	}
	h, err := m.open(ctx, scope, a, prefs, open, nil, size)
	if err != nil {
		return nil, err
	}
	if h.version.SizeBytes > 0 && h.version.SizeBytes != size {
		h.Close()
		return nil, domain.Fail("RESOURCE_CHANGED", "Cached online audio length changed")
	}
	return h, nil
}

func (h *readHandle) ReadAvailableAt(ctx context.Context, data []byte, off, size int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if h.file != nil {
		return h.file.ReadAt(data, off)
	}
	r := h.ReadCloser.(*growingReader)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readAvailable(ctx, data, off, size)
}

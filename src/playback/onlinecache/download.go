package onlinecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type OpenUpstream func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error)

// Open 合并同一表示的并行下载。等待者可独立取消；失败的下载不会发布，后续请求可重试。
func (m *Manager) Open(ctx context.Context, scope Scope, expected pluginproto.OnlineAudio, p Preferences, open OpenUpstream) (io.ReadCloser, error) {
	h, err := m.open(ctx, scope, expected, p, open, nil, 0)
	if err != nil {
		return nil, err
	}
	return h, nil
}

// Download 等待完整发布，命中时直接返回该副本的实际版本。调用方在发布前检查下载授权。
func (m *Manager) Download(ctx context.Context, scope Scope, expected pluginproto.OnlineAudio, limit int64, open OpenUpstream, check func() error) (Version, error) {
	if limit <= 0 {
		return Version{}, RetentionDisabled()
	}
	h, err := m.open(ctx, scope, expected, Preferences{LimitBytes: limit}, open, check, 0)
	if err != nil {
		return Version{}, err
	}
	defer h.Close()
	if err = ctx.Err(); err != nil {
		return Version{}, err
	}
	if check != nil {
		if err = check(); err != nil {
			return Version{}, err
		}
	}
	if h.version.ID == "" {
		return Version{}, domain.Fail("RESOURCE_CHANGED", "The download was removed while being saved")
	}
	return h.version, nil
}

func (m *Manager) open(ctx context.Context, scope Scope, expected pluginproto.OnlineAudio, p Preferences, open OpenUpstream, check func() error, rangeSize int64) (*readHandle, error) {
	key := Key(scope, expected)
	var request *downloadRequest
	var finish func()
	if p.LimitBytes > 0 {
		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			m.mu.Lock()
			// 平台没有字节版本时，必须读取本次内容；完成后按实际字节去重。
			if expected.Version != "" {
				body, err := m.lookup(ctx, key)
				if body != nil || err != nil {
					m.mu.Unlock()
					return body, err
				}
			}
			if ready := m.pending[key]; ready != nil {
				m.mu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ready.done:
					continue
				}
			}
			request = &downloadRequest{owner: scope.Owner, source: scope.Source, done: make(chan struct{}), check: check}
			m.pending[key] = request
			m.mu.Unlock()
			finish = func() { m.mu.Lock(); delete(m.pending, key); close(request.done); m.mu.Unlock() }
			break
		}
	} else {
		finish = func() {}
	}
	run, cancel := context.WithCancel(ctx)
	actual, body, err := open(run)
	if err != nil {
		cancel()
		finish()
		return nil, err
	}
	if !Matches(scope, expected, actual) {
		body.Close()
		cancel()
		finish()
		return nil, domain.Fail("RESOURCE_CHANGED", "Online audio representation changed")
	}
	if actual.SizeBytes <= 0 {
		actual.SizeBytes = expected.SizeBytes
	}
	if rangeSize > 0 {
		if actual.SizeBytes > 0 && rangeSize != actual.SizeBytes {
			body.Close()
			cancel()
			finish()
			return nil, domain.Fail("RESOURCE_CHANGED", "Online audio length changed")
		}
		actual.SizeBytes = rangeSize
	}
	downloadKey := key
	// 某些 CDN 仅在打开媒体时提供强 ETag，此时只取响应头即可复用旧文件。
	if expected.Version == "" && actual.Version != "" && p.LimitBytes > 0 {
		downloadKey = Key(scope, actual)
		m.mu.Lock()
		cached, cacheErr := m.lookup(ctx, downloadKey)
		m.mu.Unlock()
		if cached != nil || cacheErr != nil {
			body.Close()
			cancel()
			finish()
			return cached, cacheErr
		}
	}
	if p.LimitBytes == 0 && p.Stream && rangeSize == 0 {
		finish()
		return &readHandle{ReadCloser: body, release: cancel}, nil
	}
	if !p.Stream {
		defer finish()
		defer cancel()
		return m.download(run, scope, downloadKey, actual, request, body, nil)
	}
	reader := newGrowingReader()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer finish()
		defer cancel()
		stop := context.AfterFunc(run, func() { reader.abort(run.Err()) })
		defer stop()
		h, err := m.download(run, scope, downloadKey, actual, request, body, reader)
		reader.complete(h, err)
	}()
	return &readHandle{ReadCloser: reader, release: func() { cancel(); <-done }}, nil
}

func (m *Manager) download(ctx context.Context, scope Scope, key string, a pluginproto.OnlineAudio, request *downloadRequest, body io.ReadCloser, live *growingReader) (*readHandle, error) {
	stop := context.AfterFunc(ctx, func() { body.Close() })
	defer func() { stop(); body.Close() }()
	f, err := os.CreateTemp(m.root, "partial-")
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	hash := sha256.New()
	hash.Write([]byte(key))
	dest := []io.Writer{f, hash}
	if live != nil {
		live.use(f)
		defer live.use(nil)
		dest = append(dest, live)
	}
	limit := pluginproto.MaxOnlineAudioBytes
	if a.SizeBytes > 0 {
		limit = a.SizeBytes
	}
	n, err := io.Copy(io.MultiWriter(dest...), io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if n == 0 || n > pluginproto.MaxOnlineAudioBytes || a.SizeBytes > 0 && a.SizeBytes != n {
		return nil, domain.Fail("RESOURCE_CHANGED", "Incomplete or oversized online audio")
	}
	temporaryPlayback := func() (*readHandle, error) {
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		keep = true
		return &readHandle{ReadCloser: f, file: f, release: func() { os.Remove(f.Name()) }}, nil
	}
	if request == nil {
		return temporaryPlayback()
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(hash.Sum(nil))
	r := Record{Key: key, Owner: scope.Owner, Plugin: scope.Plugin, Source: scope.Source, Version: Version{ID: id, Quality: a.Quality, MIME: a.MIME, Codec: a.Codec, Container: a.Container, BitrateBps: a.BitrateBps, SampleRateHz: a.SampleRateHz, Channels: a.Channels, SizeBytes: n, DownloadedAt: time.Now().UTC()}}
	m.measure(ctx, f, &r.Version)
	if live != nil {
		live.use(nil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if request.check != nil {
		if err = request.check(); err != nil {
			return nil, err
		}
	}
	if request.deleted[id] {
		return temporaryPlayback()
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if existing, ok := m.records[id]; ok {
		// 无平台版本的并行或重复下载可能得到同一内容。
		err := m.checkFile(existing)
		if err == nil {
			return m.pin(existing)
		}
		if !os.IsNotExist(err) && !errors.Is(err, errDamaged) {
			return nil, err
		}
		if err = m.remove(ctx, existing); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(m.root, id+".audio")
	// 删除后发起的新请求已重新下载并验证相同内容，可接管旧读者仍打开的同一份物理文件。
	reuse := m.garbage[id] && m.checkFile(r) == nil
	if !reuse {
		if err = os.Rename(f.Name(), path); err != nil {
			return nil, err
		}
	}
	if err = m.repo.SaveOnlineDownload(ctx, r); err != nil {
		if !reuse {
			os.Remove(path)
		}
		return nil, err
	}
	m.records[id] = r
	delete(m.garbage, id)
	m.byKey[key] = id
	return m.pin(r)
}

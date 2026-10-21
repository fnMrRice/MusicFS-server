package logging

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

type fileSink struct {
	mu           sync.Mutex
	file         *lumberjack.Logger
	day          string
	handler      slog.Handler
	closed       bool
	size         int64
	notify       func()
	lastRotation time.Time
}

func openSink(path string, notify func()) (*fileSink, error) {
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("log file must be regular")
	}
	now := time.Now()
	archived := false
	if info != nil && info.Size() > 0 && (info.ModTime().UTC().Format(time.DateOnly) < now.UTC().Format(time.DateOnly) || info.Size() >= 100<<20) {
		if err := archivePrevious(path, info, now); err != nil {
			return nil, err
		}
		info, archived = nil, true
	}
	day := now.UTC().Format(time.DateOnly)
	if info != nil {
		day = info.ModTime().UTC().Format(time.DateOnly)
	}
	// 压缩和清理由 Manager 串行负责，关闭时可以等待归档完整落盘。
	s := &fileSink{file: &lumberjack.Logger{Filename: path, MaxSize: 100}, day: day, notify: notify}
	if archived {
		s.lastRotation = now
	}
	// 打开时验证写权限，不等第一条业务事件才暴露错误。
	if _, err := s.file.Write(nil); err != nil {
		s.file.Close()
		return nil, err
	}
	current, err := os.Stat(path)
	if err != nil {
		s.file.Close()
		return nil, err
	}
	s.size = current.Size()
	if err := s.rotateDay(time.Now()); err != nil {
		s.file.Close()
		return nil, err
	}
	s.handler = slog.NewJSONHandler(s, handlerOptions())
	notify()
	return s, nil
}

// 重启前的文件使用最后写入时间归档，避免停机数周后重新计算保留期限。
func archivePrevious(path string, info os.FileInfo, now time.Time) error {
	if info.ModTime().Before(now.Add(-RetentionDays * 24 * time.Hour)) {
		return removeArchive(path, info)
	}
	for offset := 0; offset < 1000; offset++ {
		stamp := info.ModTime().UTC().Add(time.Duration(offset) * time.Millisecond).Format(archiveTime)
		target := strings.TrimSuffix(path, ".log") + "-" + stamp + ".log"
		if !absent(target) || !absent(target+".gz") {
			continue
		}
		if !sameArchive(path, info) {
			return errors.New("log file changed before startup rotation")
		}
		return os.Rename(path, target)
	}
	return errors.New("startup log archive names are exhausted")
}

func (s *fileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("log file is closed")
	}
	if err := s.rotateDayLocked(time.Now()); err != nil {
		return 0, err
	}
	if s.size+int64(len(p)) > 100<<20 && len(p) <= 100<<20 {
		if err := s.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := s.file.Write(p)
	s.size += int64(n)
	return n, err
}

func (s *fileSink) rotateDay(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.rotateDayLocked(now)
}

func (s *fileSink) rotateDayLocked(now time.Time) error {
	day := now.UTC().Format(time.DateOnly)
	if day == s.day {
		return nil
	}
	if err := s.rotateLocked(); err != nil {
		return err
	}
	s.day = day
	return nil
}

func (s *fileSink) rotate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("log file is closed")
	}
	return s.rotateLocked()
}

func (s *fileSink) rotateLocked() error {
	if s.size == 0 {
		return nil
	}
	// 轮转名精确到毫秒，连续运维调用不得覆盖同名归档。
	if delay := time.Millisecond - time.Since(s.lastRotation); delay > 0 {
		time.Sleep(delay)
	}
	if err := s.file.Rotate(); err != nil {
		return err
	}
	s.lastRotation = time.Now()
	s.size = 0
	s.notify()
	return nil
}

func (s *fileSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.file.Close()
}

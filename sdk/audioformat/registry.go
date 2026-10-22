// Package audioformat 定义编解码插件的受限格式探测和优先级选择契约。
package audioformat

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// MaxHeaderBytes 限制一次识别读取的数据，插件只能看到这段只读头部。
const MaxHeaderBytes = 4096

// Match 表示文件内容的识别结果；RequiresDecode 表示必须先解开封装。
// PluginID 由注册表填写，不接受探测函数伪造其他插件的身份。
type Match struct {
	PluginID       string
	Format         string
	MIME           string
	RequiresDecode bool
	DurationMs     *int64
}

// Detector 由插件声明识别优先级、候选扩展名和内容探测函数。
// Probe 必须仅检查传入的头部；未识别时返回 false，不把扩展名当作内容证据。
type Detector struct {
	PluginID   string
	Priority   int
	Extensions []string
	Probe      func(header []byte, size int64) (Match, bool)
}

// Registry 创建后不可变，可供扫描、播放和后台分析并发共享。
type Registry struct {
	detectors  []Detector
	extensions map[string]struct{}
}

func New(detectors ...Detector) (*Registry, error) {
	r := &Registry{extensions: make(map[string]struct{})}
	seen := make(map[string]bool)
	for _, detector := range detectors {
		if strings.TrimSpace(detector.PluginID) == "" || detector.Probe == nil || seen[detector.PluginID] {
			return nil, fmt.Errorf("INVALID_ARGUMENT: invalid or duplicate format detector")
		}
		seen[detector.PluginID] = true
		detector.Extensions = append([]string(nil), detector.Extensions...)
		for _, extension := range detector.Extensions {
			extension = strings.ToLower(extension)
			if !strings.HasPrefix(extension, ".") || len(extension) < 2 || strings.ContainsAny(extension, "/\\ ") {
				return nil, fmt.Errorf("INVALID_ARGUMENT: invalid format extension")
			}
			r.extensions[extension] = struct{}{}
		}
		r.detectors = append(r.detectors, detector)
	}
	sort.SliceStable(r.detectors, func(i, j int) bool {
		return r.detectors[i].Priority > r.detectors[j].Priority
	})
	return r, nil
}

// Detect 只读取一次受限头部，返回优先级最高的内容匹配。
// 文件扩展名不参与检测，因此重命名或伪装扩展名不会改变识别结果。
func (r *Registry) Detect(input io.ReaderAt, size int64) (Match, bool, error) {
	if input == nil || size < 0 {
		return Match{}, false, fmt.Errorf("INVALID_ARGUMENT: invalid format probe input")
	}
	if r == nil || len(r.detectors) == 0 || size == 0 {
		return Match{}, false, nil
	}
	header, err := readHeader(input, size)
	if err != nil {
		return Match{}, false, err
	}
	for _, detector := range r.detectors {
		match, ok := detector.Probe(bytes.Clone(header), size)
		if !ok {
			continue
		}
		if match.Format == "" || match.MIME == "" || (match.DurationMs != nil && *match.DurationMs < 0) {
			return Match{}, false, fmt.Errorf("INVALID_RESPONSE: invalid format probe result")
		}
		match.PluginID = detector.PluginID
		return match, true, nil
	}
	return Match{}, false, nil
}

func readHeader(input io.ReaderAt, size int64) ([]byte, error) {
	if input == nil || size < 0 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid format probe input")
	}
	header := make([]byte, min(size, MaxHeaderBytes))
	if len(header) == 0 {
		return header, nil
	}
	n, err := input.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("SOURCE_OFFLINE: format probe input read failed: %w", err)
	}
	if n != len(header) {
		return nil, fmt.Errorf("RESOURCE_CHANGED: format probe input was truncated")
	}
	return header, nil
}

// SupportsExtension 只用于保留候选文件，不代表文件内容已通过格式验证。
func (r *Registry) SupportsExtension(path string) bool {
	if r == nil {
		return false
	}
	_, ok := r.extensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

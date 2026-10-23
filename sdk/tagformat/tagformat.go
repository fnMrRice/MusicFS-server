// Package tagformat 定义主进程内标签格式插件的输入输出，不授予文件路径或替换权限。
package tagformat

import (
	"context"
	"io"
)

// Patch 只设置提供的非空值；Overwrite 为真时才覆盖已有值。
type Patch struct {
	Comments  map[string][]string `json:"comments"`
	Artwork   []byte              `json:"artwork,omitempty"`
	Overwrite bool                `json:"overwrite"`
}

type EditResult struct {
	AudioSHA512 string
	Changed     bool
}

// AudioRange 描述无需解码即可保留的连续音频字节，宿主独立计算其摘要。
type AudioRange struct {
	Offset int64
	Size   int64
}

// Format 只读取宿主提供的源并写入未发布的目标；授权、校验、替换和恢复由宿主执行。
type Format interface {
	Descriptor() Descriptor
	ValidatePatch(Patch) error
	AudioRange(io.ReaderAt, int64) (AudioRange, error)
	Rewrite(context.Context, io.ReaderAt, int64, io.Writer, Patch) (EditResult, error)
}

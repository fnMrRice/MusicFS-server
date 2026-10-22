// Package cuesheet 解析本地 CUE 文本，返回不带文件访问能力的相对引用和逻辑音轨。
package cuesheet

import "fmt"

const (
	FramesPerSecond = 75
	MaxSheetBytes   = 1 << 20
	MaxLineBytes    = 16 << 10
	MaxTracks       = 99
	MaxFiles        = 99

	CodeInvalid     = "INVALID_CUE"
	CodeUnsupported = "UNSUPPORTED_CUE"
	CodeLimit       = "LIMIT_EXCEEDED"
)

// Sheet 仅描述导入候选，不代表引用的文件存在、可访问或已经导入。
type Sheet struct {
	Title     string  `json:"title,omitempty"`
	Performer string  `json:"performer,omitempty"`
	Files     []File  `json:"files"`
	Tracks    []Track `json:"tracks"`
}

// File.Path 是以 / 分隔的包内相对引用，宿主仍须检查授权范围和符号链接。
type File struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// Track 的所有位置均为所属文件时间轴上的精确 CUE 帧数。
// EndFrames 不含结束帧；缺省时必须由宿主探测文件时长后确定。
type Track struct {
	Number        int    `json:"number"`
	FileIndex     int    `json:"fileIndex"`
	Title         string `json:"title,omitempty"`
	Performer     string `json:"performer,omitempty"`
	Index00Frames *int64 `json:"index00Frames,omitempty"`
	StartFrames   int64  `json:"startFrames"`
	EndFrames     *int64 `json:"endFrames,omitempty"`
}

// Error 提供稳定的错误类别和一基行号，不回显文件路径或输入内容。
// Line 为 0 表示错误涉及整个文档。
type Error struct {
	Code    string `json:"code"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s at line %d: %s", e.Code, e.Line, e.Message)
	}
	return e.Code + ": " + e.Message
}

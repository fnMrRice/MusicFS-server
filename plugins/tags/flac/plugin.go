// Package flac 提供主进程内的 FLAC 标签插件，保留原始音频和未选择的元数据。
package flac

import (
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/tagformat"
)

type Plugin struct{}

var _ tagformat.Format = (*Plugin)(nil)

func New() *Plugin { return &Plugin{} }

func invalid(message string) error     { return fault.Fail("INVALID_ARGUMENT", message) }
func unsupported(message string) error { return fault.Fail("UNSUPPORTED", message) }

func (*Plugin) ValidatePatch(p tagformat.Patch) error { return validatePatch(p) }

func (*Plugin) AudioRange(src io.ReaderAt, size int64) (tagformat.AudioRange, error) {
	file, err := readFLAC(src, size)
	if err != nil {
		return tagformat.AudioRange{}, err
	}
	return tagformat.AudioRange{Offset: file.audioOffset, Size: size - file.audioOffset}, nil
}

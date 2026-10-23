package enrichment

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// Input 只包含宿主授权的单任务资源，不向插件暴露路径。
type Input struct {
	Resource Resource
	Reader   io.ReaderAt
	Validate func() error
}

// Descriptor 由插件声明身份与能力，业务层不按具体插件名称推断行为。
type Descriptor struct {
	PluginID                  string
	Fields                    []string
	QueryCorrectionsSupported bool
}

func (d Descriptor) Validate() error {
	if !validText(d.PluginID, 128, true) || len(d.Fields) == 0 || len(d.Fields) > 2 {
		return fmt.Errorf("INVALID_ARGUMENT: invalid scraper descriptor")
	}
	for i, field := range d.Fields {
		if (field != "lyrics" && field != "artwork") || slices.Contains(d.Fields[:i], field) {
			return fmt.Errorf("INVALID_ARGUMENT: invalid scraper fields")
		}
	}
	return nil
}

func (d Descriptor) Supports(fields []string, query *Query) bool {
	if query != nil && (query.Title != "" || len(query.Artists) > 0) && !d.QueryCorrectionsSupported {
		return false
	}
	for _, field := range fields {
		if !slices.Contains(d.Fields, field) {
			return false
		}
	}
	return true
}

// Host 和 Session 是刮削调度使用的契约，具体通信及插件实现自行选择。
type Host interface {
	Descriptor() Descriptor
	PluginVersion() string
	Open(context.Context, pluginproto.Context, Target, []Input) (Session, error)
}

type Session interface {
	PluginVersion() string
	Search(context.Context, []string, []string, *Query) (SearchResult, error)
	Fetch(context.Context, string, io.Writer) (Candidate, error)
	Close() error
}

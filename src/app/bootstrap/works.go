package bootstrap

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// configureWorks 在装配边界选择内置类型，作品服务不硬编码音乐或 ASMR 规则。
func configureWorks(handler *api.Server, repository works.Repository) error {
	registry, err := contenttypes.New(music.Descriptor(), asmr.Descriptor())
	if err != nil {
		return err
	}
	handler.RegisterWorks(works.New(repository, registry))
	return nil
}

package bootstrap

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

// configureWorks 在装配边界选择内置类型，作品服务不硬编码音乐或 ASMR 规则。
func configureWorks(handler *api.Server, repository storage.Backend, sources *library.Service) error {
	registry, err := contenttypes.New(music.Descriptor(), asmr.Descriptor())
	if err != nil {
		return err
	}
	handler.RegisterWorks(works.New(repository, registry))
	handler.RegisterWorkPackages(workpackages.New(repository, repository, sources, map[string]contenttype.PackageOrganizer{
		"asmr": asmr.PackageOrganizer(), "music": music.PackageOrganizer(),
	}))
	return nil
}

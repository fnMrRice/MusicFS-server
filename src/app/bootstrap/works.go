package bootstrap

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/cdalbum"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

// configureWorks 在装配层注册作品库插件与普通音乐的资料声明。
func configureWorks(handler *api.Server, repository storage.Backend, sources *library.Service) error {
	registry, err := contenttypes.New(cdalbum.Descriptor(), asmr.Descriptor(), radio.Descriptor(), music.Descriptor())
	if err != nil {
		return err
	}
	handler.RegisterWorks(works.New(repository, registry))
	handler.RegisterWorkPackages(workpackages.New(repository, repository, sources, map[string]contenttype.PackageOrganizer{
		cdalbum.TypeID: cdalbum.PackageOrganizer(), asmr.TypeID: asmr.PackageOrganizer(),
		radio.TypeID: radio.PackageOrganizer(), music.TypeID: music.PackageOrganizer(),
	}))
	return nil
}

package bootstrap

import (
	"context"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/cdalbum"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/plugins/builtinplugins"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

// configureWorks 在装配层注册作品库插件。
func configureWorks(ctx context.Context, cfg config.Config, handler *api.Server, repository storage.Backend, sources *library.Service) (func(), error) {
	registry, err := contenttypes.New(builtinplugins.ContentTypes()...)
	if err != nil {
		return nil, err
	}
	person := personal.New(repository)
	histories := map[string]func(contenttype.HistoryState) contenttype.History{
		cdalbum.TypeID: cdalbum.History, asmr.TypeID: asmr.History, radio.TypeID: radio.History,
	}
	var states []plugindb.Backend
	closeHistory := func() {
		for _, state := range states {
			_ = state.Close()
		}
	}
	for _, d := range registry.List() {
		if d.Library == nil || !d.Library.SeparateHistory {
			continue
		}
		open := histories[d.ID]
		if open == nil {
			closeHistory()
			return nil, fmt.Errorf("content type %s requires a history provider", d.ID)
		}
		state, err := plugindb.OpenBackend(ctx, config.PluginDatabaseBackend(cfg), config.PluginDatabasePath(cfg),
			d.PluginID, instanceid.Canonical("content-"+d.ID))
		if err != nil {
			closeHistory()
			return nil, err
		}
		states = append(states, state)
		person.RegisterHistory(d.ID, open(state))
		if err := person.DeliverHistory(ctx, "", d.ID); err != nil {
			closeHistory()
			return nil, err
		}
	}
	handler.RegisterPersonal(person)
	handler.RegisterWorks(works.New(repository, registry))
	handler.RegisterWorkPackages(workpackages.New(repository, repository, sources, map[string]contenttype.PackageOrganizer{
		cdalbum.TypeID: cdalbum.PackageOrganizer(), asmr.TypeID: asmr.PackageOrganizer(),
		radio.TypeID: radio.PackageOrganizer(),
	}))
	return closeHistory, nil
}

package ncmdecoder

import (
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func init() {
	if err := codecsettings.Register(settings.Provider{ID: "musicfs.plugin.decoder.ncm", Schema: SettingsSchema()}, ""); err != nil {
		panic(err)
	}
	if err := audiocodecs.Register((*Processor)(nil).AudioDecoder(), nil); err != nil {
		panic(err)
	}
}

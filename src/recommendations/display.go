package recommendations

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
)

func runtimeName(r Runtime) string {
	name := strings.TrimSpace(r.Name)
	if utf8.ValidString(name) && utf8.RuneCountInString(name) > 0 && utf8.RuneCountInString(name) <= 128 && !strings.ContainsFunc(name, unicode.IsControl) {
		return name
	}
	if identity := pluginconfig.Identity(pluginproto.OnlinePluginID(r.Platform)); identity != nil {
		return identity.Name
	}
	return "Unknown source"
}

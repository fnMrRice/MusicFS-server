package plugincontrol

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

// DSP 的显示身份来自节点声明；官方装配可额外提供译名。
func dspIdentity(d audio.Descriptor) *pluginproto.PluginIdentity {
	if identity := pluginconfig.Identity(d.PluginID); identity != nil {
		return identity
	}
	name := d.DisplayName
	if name == "" {
		name = d.Name
	}
	return &pluginproto.PluginIdentity{ID: d.PluginID, Name: name, LocalizedNames: map[string]string{}, Features: []string{"audio-dsp"}}
}

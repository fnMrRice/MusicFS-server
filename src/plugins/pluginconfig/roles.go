package pluginconfig

import "github.com/fnMrRice/MusicFS-server/sdk/pluginproto"

func Role(pluginID string) string {
	d, _ := Lookup(pluginID)
	return d.Role
}

// SupportsMultipleInstances 返回可信装配声明的独立多实例能力。
func SupportsMultipleInstances(pluginID string) bool {
	d, _ := Lookup(pluginID)
	return d.SupportsMultipleInstances
}

func SupportsDirectories(id string) bool { d, _ := Lookup(id); return d.Directories }
func NeedsAudioTools(id string) bool     { d, _ := Lookup(id); return d.AudioTools }
func ProcessesAudio(id string) bool      { d, _ := Lookup(id); return d.AudioProcessing }
func Version(id string) string           { d, _ := Lookup(id); return d.Version }
func HasSettings(id string) bool         { d, _ := Lookup(id); return d.Settings }

// Configuration 返回插件声明的独立副本，不允许调用者改写注册数据。
func Configuration(id string) *pluginproto.Configuration {
	d, ok := Lookup(id)
	if !ok || d.Configuration == nil {
		return nil
	}
	return d.Configuration()
}

// ValidateManifest 将 Worker 声明与可信装配中的角色和来源类型核对。
func ValidateManifest(manifest pluginproto.Manifest, role string) error {
	if err := manifest.Validate(role); err != nil {
		return err
	}
	d, ok := Lookup(manifest.PluginID)
	if !ok || d.Role != role || manifest.SupportsMultipleInstances != d.SupportsMultipleInstances || manifest.SourceType != d.SourceType {
		return pluginproto.Failure("PROTOCOL_MISMATCH", "Plugin instance support does not match its declaration")
	}
	return nil
}

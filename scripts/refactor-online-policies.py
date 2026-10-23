from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
def edit(name, transform):
    p = ROOT / name
    p.write_text(transform(p.read_text(encoding='utf-8')), encoding='utf-8', newline='\n')

def validators(s):
    s = s.replace('platform string', 'policy ResourcePolicy').replace('(platform,', '(policy,')
    start = s.index('\tvalidID :=', s.index('func ValidateTrack'))
    end = s.index('\ttitle, err :=', start)
    s = s[:start] + '''\twantURL := ReferenceURL(policy.Tracks, track.ExternalID)
\tif wantURL == "" || track.SourceURL != wantURL || track.PlaybackAvailability != "unknown" || track.Artists == nil || len(track.Artists) > 128 {
\t\treturn invalidResult()
\t}
''' + s[end:]
    start = s.index('\twantURL :=', s.index('func validatePlaylist'))
    end = s.index('\tif wantURL ==', start)
    s = s[:start] + '\twantURL := ReferenceURL(policy.Playlists, p.ExternalID)\n' + s[end:]
    s = s.replace('validImageReference(p.CoverURL)', 'validImageReference(policy, p.CoverURL)')
    start = s.index('\t\tvalidID :=', s.index('func ValidateObservation'))
    end = s.index('\t\tif entry.Position', start)
    s = s[:start] + '\t\tvalidID := policy.ValidPlaylistEntry(entry.ExternalTrackID)\n' + s[end:]
    s = s.replace('platform != Netease || ', '')
    return s
edit('sdk/online/validation.go', validators)

def display(s):
    s = s.replace('platform string', 'policy ResourcePolicy').replace('(platform,', '(policy,')
    s = s.replace('func validImageReference(value', 'func validImageReference(policy ResourcePolicy, value').replace('ImageURL(*value)', 'policy.ImageURL(*value)')
    s = s.replace('platform != Netease || !numericID.MatchString(a.ExternalID)', 'ReferenceURL(policy.Albums, a.ExternalID) == ""', 1)
    s = s.replace('"https://music.163.com/album?id="+a.ExternalID', 'ReferenceURL(policy.Albums, a.ExternalID)')
    s = s.replace('platform != Netease || !numericID.MatchString(a.ExternalID)', 'ReferenceURL(policy.Artists, a.ExternalID) == ""', 1)
    s = s.replace('"https://music.163.com/artist?id="+a.ExternalID', 'ReferenceURL(policy.Artists, a.ExternalID)')
    s = s.replace('validImageReference(a.', 'validImageReference(policy, a.')
    return s
edit('sdk/online/display_validation.go', display)

def helpers(s):
    start = s.index('import (')
    end = s.index('func SearchKind', start)
    s = s[:start]+s[end:]
    start = s.index('// ImageURL')
    end = s.index('// ValidCount', start)
    return s[:start]+s[end:]
edit('sdk/online/helpers.go', helpers)

def models(s):
    start = s.index('// Capabilities defines')
    end = s.index('// TextValue', start)
    return s[:start] + s[end:]
edit('sdk/online/models.go', models)

for name in ['online_calls.go', 'online_entity.go', 'online_display.go']:
    edit('src/plugins/pluginhost/'+name, lambda s: re.sub(r'(online\.Validate\w+\()w\.config\.Platform,', r'\1w.descriptor.Resources,', s))

def playlists(s):
    s = s.replace('\n\t"regexp"', '')
    s = s.replace('var playlistExternalID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)\n\n', '')
    start = s.index('\tvalidID := playlistExternalID')
    end = s.index('\tif !validID', start)
    return s[:start] + '\tvalidID := online.ReferenceURL(runtime.descriptor.Resources.Playlists, ref.ExternalID) != ""\n' + s[end:]
edit('src/plugins/onlinehost/playlists.go', playlists)

def proto(s):
    s = s.replace('\n\t"bytes"', '')
    s = s.replace('\tCapabilities   []online.Capability `json:"capabilities"`', '\tCapabilities   []online.Capability `json:"capabilities"`\n\tResources online.ResourcePolicy `json:"resources"`\n\tLogin *online.LoginDescriptor `json:"login,omitempty"`')
    start = s.index('func OnlinePluginID(')
    end = s.index('func (d OnlineDescriptor) Validate', start)
    s = s[:start]+'''// OnlinePluginID 只校验平台标识格式；插件准入由宿主受信任注册表管理。
func OnlinePluginID(platform string) string {
\tif !regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`).MatchString(platform) { return "" }
\treturn "musicfs.plugin.source." + platform
}

// OnlinePolicy 提供平台无关的空描述符；能力和资料策略必须由插件补充。
func OnlinePolicy(platform string) OnlineDescriptor {
\treturn OnlineDescriptor{
\t\tPlatform: platform, SourceType: SourceNetwork, Authentication: "platformAccountRequired", Capabilities: []online.Capability{}, SchemaRevision: OnlineRevision,
\t\tConfigSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}, ConfigDefaults: map[string]any{},
\t}
}

''' + s[end:]
    start = s.index('\t// Identity and explanatory text')
    end = s.index('\n\treturn nil', start)
    s = s[:start]+'''\tif err := validateOnlineCapabilities(d.Capabilities); err != nil { return err }
\tif d.Platform != platform || d.SourceType != SourceNetwork || d.SchemaRevision != OnlineRevision || (d.Authentication != "anonymous" && d.Authentication != "platformAccountRequired") || d.Resources.Validate() != nil || d.Login.Validate() != nil {
\t\treturn Failure("PROTOCOL_MISMATCH", "Invalid online descriptor")
\t}
\tif d.ConfigSchema == nil || d.ConfigDefaults == nil { return Failure("PROTOCOL_MISMATCH", "Online configuration declaration is required") }
''' + s[end:]
    start = s.index('func OnlineCapabilities(')
    end = s.index('func ValidateOnlineManifest', start)
    s = s[:start]+'''// OnlineCapabilities 从插件声明导出实际启用能力，不维护任何平台能力上限。
func OnlineCapabilities(descriptor OnlineDescriptor) []string {
\tresult := []string{}
\tfor _, c := range descriptor.Capabilities { if c.Supported { result = append(result, c.Name) } }
\treturn result
}

''' + s[end:]
    s = s.replace('OnlineCapabilities(platform)', 'online.CapabilityNames()')
    return s
edit('sdk/pluginproto/online.go', proto)
edit('sdk/pluginproto/online_compatibility.go', lambda s: s.replace('func validateOnlineCapabilities(platform string, capabilities', 'func validateOnlineCapabilities(capabilities').replace('for _, capability := range online.Capabilities(platform) {\n\t\tpolicy[capability.Name] = capability.Supported', 'for _, name := range online.CapabilityNames() {\n\t\tpolicy[name] = true').replace('Online capability exceeds verified host policy', 'Invalid online capability declaration'))

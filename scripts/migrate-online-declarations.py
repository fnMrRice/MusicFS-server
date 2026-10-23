from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
def edit(name, transform):
    p = ROOT / name
    p.write_text(transform(p.read_text(encoding='utf-8')), encoding='utf-8', newline='\n')
def add_import(s, path, alias=''):
    if '"'+path+'"' in s: return s
    line = '\t'+(alias+' ' if alias else '')+'"'+path+'"\n'
    if 'import (' in s: return s.replace('import (\n', 'import (\n'+line, 1)
    return re.sub(r'import ([^\n]+)\n', 'import (\n'+line+r'\t\1\n)\n', s, count=1)

for platform, const in [('netease','Netease'), ('qqmusic','QQMusic'), ('applemusic','AppleMusic'), ('bilibili','Bilibili')]:
    def plugin(s):
        s = add_import(s, 'github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies')
        variable = 'd' if platform == 'bilibili' else 'descriptor'
        statement = f'{variable} := pluginproto.OnlinePolicy(online.{const})'
        extra = f'\n\t{variable}.Resources = policies.Resources(online.{const})\n\t{variable}.Capabilities = policies.Capabilities(online.{const})'
        if platform == 'bilibili': extra += f'\n\t{variable}.Authentication = "anonymous"'
        else:
            method = 'musicKit' if platform == 'applemusic' else 'qr'
            extra += f'\n\t{variable}.Login = &online.LoginDescriptor{{Method: "{method}", Validation: "liveAccountPending"}}'
        return s.replace(statement, statement+extra)
    edit(f'plugins/source/online/{platform}/plugin.go', plugin)

def adapter(s):
    s = s.replace('import sdkonline "github.com/fnMrRice/MusicFS-server/sdk/online"', 'import (\n sdkonline "github.com/fnMrRice/MusicFS-server/sdk/online"\n "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"\n)')
    s = s.replace('sdkonline.Capabilities(platform)', 'policies.Capabilities(platform)')
    s = re.sub(r'(sdkonline\.Validate\w+\()platform,', r'\1policies.Resources(platform),', s)
    return s.replace('sdkonline.ImageURL(value)', 'policies.Netease().ImageURL(value)')
edit('plugins/source/online/internal/adapter/sdk.go', adapter)
edit('plugins/source/online/netease/driver/models.go', lambda s: add_import(s.replace('online.ImageURL(value)', 'policies.Netease().ImageURL(value)'), 'github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies'))

# 测试夹具引用插件声明，不再重建宿主的平台策略。
for root in ['tests', 'plugins/source/online']:
    for p in (ROOT/root).rglob('*.go'):
        if not (p.name.endswith('_test.go') or 'testkit' in p.parts or 'testapps' in p.parts): continue
        s = p.read_text(encoding='utf-8')
        old = s
        # 插件内部测试可能已使用适配器包装层，该层继续接收平台参数。
        if '"github.com/fnMrRice/MusicFS-server/sdk/online"' in s:
            s = re.sub(r'(online\.Validate\w+\()(online\.(?:Netease|QQMusic|AppleMusic|Bilibili)|"bilibili"),', r'\1onlineplugins.Describe(\2).Resources,', s)
        s = re.sub(r'pluginproto\.OnlineCapabilities\((online\.(?:Netease|QQMusic|AppleMusic|Bilibili)|platform)\)', r'pluginproto.OnlineCapabilities(onlineplugins.Describe(\1))', s)
        s = re.sub(r'pluginproto\.OnlinePolicy\((online\.(?:Netease|QQMusic|AppleMusic|Bilibili))\)', r'onlineplugins.Describe(\1)', s)
        s = s.replace('online.BilibiliTrackURL(', 'online.ReferenceURL(onlineplugins.Describe(online.Bilibili).Resources.Tracks, ')
        s = s.replace('online.BilibiliPlaylistURL(', 'online.ReferenceURL(onlineplugins.Describe(online.Bilibili).Resources.Playlists, ')
        if s != old:
            # Bilibili 包内测试不能反向导入父级装配包。
            if p.parent.name == 'bilibili':
                s = s.replace('onlineplugins.Describe("bilibili")', 'Plugin(Options{}).Descriptor')
            else: s = add_import(s, 'github.com/fnMrRice/MusicFS-server/plugins/source/online', 'onlineplugins')
            p.write_text(s, encoding='utf-8', newline='\n')

edit('tests/unit/onlinehost/bilibili_test.go', lambda s: add_import(s.replace('func (p *anonymousBilibili) Metadata(', 'func (p *anonymousBilibili) Descriptor() pluginproto.OnlineDescriptor { return onlineplugins.Describe(online.Bilibili) }\n\nfunc (p *anonymousBilibili) Metadata('), 'github.com/fnMrRice/MusicFS-server/sdk/pluginproto'))

def sdkvalidation(s):
    s = s.replace('online.ValidateTrack(online.Netease,', 'online.ValidateTrack(fixturePolicy(),').replace('online.ValidateObservation(online.Netease,', 'online.ValidateObservation(fixturePolicy(),')
    s = s.replace('online.ValidateTrack("third-party",', 'online.ValidateTrack(online.ResourcePolicy{},')
    s += '''
func fixturePolicy() online.ResourcePolicy {
 return online.ResourcePolicy{
 Tracks: []online.ReferenceRule{{IDPattern:`^([1-9][0-9]{0,19})$`, URLTemplate:"https://music.163.com/song?id=${1}"}},
 Playlists: []online.ReferenceRule{{IDPattern:`^([1-9][0-9]{0,19})$`, URLTemplate:"https://music.163.com/playlist?id=${1}"}},
 ImageHosts: []string{"*.music.126.net"},
 }
}
'''
    return s
edit('sdk/online/validation_test.go', sdkvalidation)
def sdkbili(s):
    s = s.replace('online.BilibiliTrackURL(', 'online.ReferenceURL(bilibiliPolicy().Tracks, ').replace('online.BilibiliPlaylistURL(', 'online.ReferenceURL(bilibiliPolicy().Playlists, ')
    s = s.replace('online.ValidateTrack(online.Bilibili,', 'online.ValidateTrack(bilibiliPolicy(),').replace('online.ValidateObservation(online.Bilibili,', 'online.ValidateObservation(bilibiliPolicy(),')
    policy = (ROOT/'plugins/source/online/internal/policies/bilibili.go').read_text(encoding='utf-8').split('func Bilibili()',1)[1]
    return s+'\nfunc bilibiliPolicy()'+policy
edit('sdk/online/bilibili_test.go', sdkbili)
edit('sdk/online/bilibili.go', lambda s: '// 平台身份规则由各插件的 ResourcePolicy 声明；SDK 仅执行通用规则。\npackage online\n')

edit('plugins/source/online/plugin.go', lambda s: s+'''
// Descriptors 返回官方插件的独立声明，供受信任装配建立注册表。
func Descriptors() []pluginproto.OnlineDescriptor {
 return []pluginproto.OnlineDescriptor{netease.Plugin(Options{}).Descriptor, qqmusic.Plugin(Options{}).Descriptor, applemusic.Plugin(Options{}).Descriptor, bilibili.Plugin(Options{}).Descriptor}
}
''')

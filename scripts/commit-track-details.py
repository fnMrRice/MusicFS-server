"""精确提交来源详情功能，保留其他任务的工作树与暂存区。"""
import importlib.util
import json
import sys
from pathlib import Path

SCRIPT = Path(__file__).resolve()
spec = importlib.util.spec_from_file_location('selective_commit', SCRIPT.with_name('commit-bulk-merge.py'))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
helper.OUTPUT = helper.SERVER / 'build/track-details-commit'
helper.MESSAGES = {
    'server': 'feat(provider): 由来源插件提供曲目详细信息',
    'web': 'feat(library): 在来源版本中展示插件详情',
    'parent': 'feat(library): 同步来源详情架构与实现版本',
}
helper.SERVER_FILES = '''docs/README.md
docs/contracts/http.schema.json
docs/contracts/openapi.yaml
docs/contracts/plugins/v1/README.md
docs/contracts/plugins/v1/online.schema.json
docs/contracts/plugins/v1/protocol.schema.json
docs/library/source-track-details.md
plugins/source/localfiles/worker.go
plugins/source/localfiles/track_details.go
plugins/source/online/README.md
plugins/source/online/internal/adapter/authorized.go
plugins/source/online/internal/session/driver.go
plugins/source/online/internal/session/unsupported.go
plugins/source/online/netease/driver/tracks.go
plugins/source/online/netease/driver/track_details.go
plugins/source/online/tests/online/track_details_test.go
plugins/source/online/tests/provider/track_details_test.go
sdk/online/models.go
sdk/pluginproto/track_details.go
sdk/pluginproto/track_details_test.go
sdk/provider/capabilities.go
sdk/provider/plugin.go
sdk/provider/unimplemented.go
sdk/provider/worker.go
sdk/provider/track_details_test.go
src/app/api/library.go
src/app/api/online.go
src/library/service.go
src/library/track_details.go
src/plugins/onlinehost/track_details.go
src/plugins/pluginhost/online_capabilities.go
src/plugins/pluginhost/track_details.go
tests/contract/api/online_track_details_test.go
tests/contract/api/track_details_test.go
tests/contract/plugins/track_details_test.go
scripts/commit-track-details.py'''.splitlines()
helper.WEB_FILES = '''docs/design.md
docs/track-details.md
src/api/client.ts
src/api/track-details.ts
src/features/matching/TrackVersions.tsx
src/features/matching/TrackDetails.tsx
src/features/matching/track-details.css
src/i18n/matching.ts
tests/unit/api/track-details.test.ts
tests/e2e/track-details-mock.spec.ts
tests/e2e/track-details-real.spec.ts'''.splitlines()


def after(base, anchor, added):
    if base.count(anchor) != 1 or added in base:
        raise RuntimeError('Snapshot insertion point is ambiguous or already committed')
    return base.replace(anchor, anchor + added, 1)


def selected_json(base, current, container, keys):
    """按完整单行成员替换，保留无关成员的原始格式与内容。"""
    value = base
    additions = []
    before, source = json.loads(base)[container], json.loads(current)[container]
    for key in keys:
        prefix = '    ' + json.dumps(key) + ':'
        new = helper.one_line(current, prefix)
        if key in before:
            value = value.replace(helper.one_line(base, prefix), new, 1)
        else:
            additions.append(new + '\n')
    if additions:
        value = after(value, '  ' + json.dumps(container) + ': {\n', ''.join(additions))
    expected = json.loads(base)
    expected[container].update({key: source[key] for key in keys})
    if json.loads(value) != expected:
        raise RuntimeError('Snapshot JSON contains unexpected changes')
    return value


def snapshot(repo, name, head):
    current = (repo / name).read_text(encoding='utf-8')
    shared = {
        helper.SERVER: {'docs/README.md', 'docs/contracts/http.schema.json', 'docs/contracts/openapi.yaml',
                        'docs/contracts/plugins/v1/README.md', 'docs/contracts/plugins/v1/protocol.schema.json'},
        helper.ROOT / 'web': {'docs/design.md'},
        helper.ROOT: {'docs/architecture.md'},
    }
    if name not in shared.get(repo, set()):
        return (repo / name).read_bytes()
    base = helper.git(repo, 'show', f'{head}:{name}').decode('utf-8')
    if name == 'docs/contracts/http.schema.json':
        value = selected_json(base, current, '$defs', [
            'TrackDetailField', 'TrackDetailSection', 'TrackDetails', 'SourceTrackDetails',
            'OnlineCapability', 'OnlineDescriptor', 'OnlineInstance',
        ])
    elif name == 'docs/contracts/openapi.yaml':
        value = selected_json(base, current, 'paths', [
            '/source-tracks/{id}/details', '/online-instances/{id}/tracks/{externalId}/details',
        ])
    elif name == 'docs/contracts/plugins/v1/protocol.schema.json':
        # 排除另一个任务的 AudioStart 修改，并检查其余定义仅包含本功能。
        before, source = json.loads(base), json.loads(current)
        new = helper.one_line(current, '      "properties": {"disableEffects":')
        old = helper.one_line(base, '      "properties": {"plan": {"$ref": "#/$defs/AudioPlan"}')
        value = current.replace(new, old, 1)
        expected = json.loads(base)
        for key in ['TrackDetailField', 'TrackDetailSection', 'TrackDetails', 'Request', 'Success']:
            expected['$defs'][key] = source['$defs'][key]
        if json.loads(value) != expected:
            raise RuntimeError('Unexpected plugin protocol changes')
    else:
        prefixes = {
            (helper.SERVER, 'docs/README.md'): ('| [录音聚合曲库]', '| [来源曲目详情]', '\n', ''),
            (helper.SERVER, 'docs/contracts/plugins/v1/README.md'):
                ('观测字段中必需但可为', '`track.details` 能力', '\n\n', ''),
            (helper.ROOT / 'web', 'docs/design.md'):
                ('音乐源标签将本地与在线实例', '重复歌曲展开后的每个来源版本', '\n\n', ''),
            (helper.ROOT, 'docs/architecture.md'):
                ('Web 曲目列表由 Server 在分页前', '展开后的每个来源版本提供', '\n\n', ''),
        }
        anchor, prefix, spacing, suffix = prefixes[(repo, name)]
        value = after(base, helper.one_line(base, anchor), spacing + helper.one_line(current, prefix) + suffix)
    return value.encode('utf-8')


helper.snapshot = snapshot

if __name__ == '__main__':
    if len(sys.argv) != 3 or sys.argv[1] not in ('prepare', 'commit') or sys.argv[2] not in helper.REPOSITORIES:
        raise SystemExit('Usage: commit-track-details.py prepare|commit server|web|parent')
    {'prepare': helper.prepare, 'commit': helper.commit}[sys.argv[1]](sys.argv[2])

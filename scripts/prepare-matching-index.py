"""仅暂存本任务改动，保留工作树中的无关编辑。"""
import json
import subprocess
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
temp = root / 'build' / 'matching-index-files'
temp.mkdir(parents=True, exist_ok=True)


def git(*args, data=None):
    return subprocess.check_output(['git', *args], cwd=root, input=data)


def base(path):
    return git('show', 'HEAD:' + path).decode('utf-8')


def index_text(path, text):
    blob = git('hash-object', '-w', '--stdin', data=text.encode('utf-8')).decode().strip()
    git('update-index', '--add', '--cacheinfo', '100644', blob, path)


owned = [
    'docs/library/duplicate-detection.md', 'docs/library/recording-groups.md',
    'docs/plugins/settings.md', 'docs/metadata/matching.md', 'docs/plugins/plugin-sdk.md',
    'sdk/README.md', 'sdk/pluginsettings/schema.go',
    'plugins/database/sqlite/library_grouped.go',
    'plugins/database/sqlite/matching_automation.go',
    'plugins/database/sqlite/matching_candidate_page.go',
    'plugins/database/sqlite/matching_candidates.go',
    'plugins/database/sqlite/matching_display.go',
    'plugins/database/sqlite/plugin_settings.go',
    'plugins/database/sqlite/migrations/020_batch_011_020.sql',
    'plugins/database/sqlite/migrations/021_batch_011_021.sql',
    'plugins/database/sqlite/migrations/021_scan_retry.sql',
    'plugins/database/sqlite/migrations/025_matching_settings.sql',
    'plugins/database/sqlite/migrations/README.md',
    'plugins/matching/standard/settings.go',
    'scripts/update-matching-contracts.py',
    'src/app/api/library.go', 'src/app/api/matching.go', 'src/app/api/plugin_settings.go',
    'src/library/grouped.go', 'src/library/models.go',
    'src/metadata/matching/automation.go', 'src/metadata/matching/automation_models.go',
    'src/metadata/matching/models.go', 'src/metadata/matching/service.go',
    'src/plugins/plugincontrol/models.go', 'src/plugins/settings/models.go', 'src/plugins/settings/service.go',
    'src/storage/backend.go',
    *['src/storage/rpcbackend/client_%02d.go' % i for i in range(3, 8)],
    'tests/contract/api/library_test.go', 'tests/contract/api/matching_http_test.go',
    'tests/contract/api/plugin_settings_test.go',
    'tests/integration/storage/library_grouped_test.go',
    'tests/integration/storage/matching_automation_test.go',
    'tests/integration/storage/migration_batches_test.go',
    'tests/integration/storage/plugin_settings_test.go',
]
# 原迁移路径已经由 git mv 从索引中移除。
owned = [p for p in owned if (root / p).exists() or git('ls-files', '--', p).strip()]
git('add', '--', *owned)

path = 'src/app/bootstrap/composition.go'
text = base(path)
text = text.replace('"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"',
                    '"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"\n\t"github.com/fnMrRice/MusicFS-server/src/plugins/settings"')
text = text.replace('\thandler.RegisterMatching(matching.New(store, fingerprintDetails, standard.Plugin{}))',
'''\tmatchingService := matching.New(store, fingerprintDetails, standard.Plugin{})
\thandler.RegisterMatching(matchingService)
\tmatchingAutomation := matching.NewAutomation(store, store, matchingService, standard.Detector())
\thandler.RegisterPluginSettings(settings.New(store, matchingAutomation.Provider()))
\tbeforeMatching := cleanup
\tcleanup = func() { matchingAutomation.Close(); beforeMatching() }''')
text = text.replace('\treturn handler, cleanup, nil', '\tmatchingAutomation.Start(ctx)\n\treturn handler, cleanup, nil')
index_text(path, text)

path = 'src/plugins/plugincontrol/builtin.go'
text = base(path).replace('Plugin{PluginID: item.id,', 'Plugin{SettingsAvailable: item.id == "musicfs.plugin.matching.standard", PluginID: item.id,')
index_text(path, text)

path = 'docs/README.md'
text = base(path)
for anchor, added in [
    ('| [实例配置与目录](plugins/plugin-configuration.md) | 插件配置数据库、目录授权与管理 API |',
     '| [声明式设置页面](plugins/settings.md) | 插件字段、按钮、值校验与持久化 |'),
    ('| [曲库查询](library/catalog-query.md) | 搜索、筛选、排序和固定结果分页 |',
     '| [录音聚合曲库](library/recording-groups.md) | 人工合并后按歌曲显示与多版本展开 |\n| [自动重复识别](library/duplicate-detection.md) | 插件候选、后台识别、进度与人工确认 |')]:
    assert anchor in text
    text = text.replace(anchor, anchor + '\n' + added)
index_text(path, text)

path = 'docs/operations/configuration.md'
text = base(path).replace('查找选项按目录保存，不自动扫描；插件管理摘要独立统计允许访问的根目录数量',
                         '查找选项按目录保存，Web 在添加或编辑配置应用成功后自动扫描；直接调用配置 API 不触发扫描。插件管理摘要独立统计允许访问的根目录数量')
index_text(path, text)

path = 'docs/operations/running.md'
text = base(path)
line = next(line for line in (root / path).read_text(encoding='utf-8').splitlines() if line.startswith('- FFmpeg 插件及实例两层启用'))
text = '\n'.join(line if old.startswith('- FFmpeg 实例配置') else old for old in text.split('\n'))
index_text(path, text)

# 对 HEAD 副本执行本任务的契约更新，不改动共享工作树文件。
contract_root = temp / 'contracts-only'
(contract_root / 'scripts').mkdir(parents=True, exist_ok=True)
(contract_root / 'docs/contracts').mkdir(parents=True, exist_ok=True)
updater = contract_root / 'scripts/update-matching-contracts.py'
updater.write_bytes((root / 'scripts/update-matching-contracts.py').read_bytes())
for path in ['docs/contracts/http.schema.json', 'docs/contracts/openapi.yaml']:
    (contract_root / path).write_text(base(path), encoding='utf-8')
subprocess.check_call([sys.executable, str(updater)], cwd=contract_root)
for path in ['docs/contracts/http.schema.json', 'docs/contracts/openapi.yaml']:
    index_text(path, (contract_root / path).read_text(encoding='utf-8'))

print('Staged matching, grouped library, declarative settings, and local playback documentation.')

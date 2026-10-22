"""暂存编解码改动，保留同一工作树内尚未完成的 DSP 设置改动。"""
import subprocess
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]


def git(*args, data=None):
    return subprocess.check_output(['git', *args], cwd=root, input=data)


def base(path):
    return git('show', 'HEAD:' + path).decode('utf-8')


def current(path):
    return (root / path).read_text(encoding='utf-8')


def replace_once(text, old, new=''):
    assert text.count(old) == 1, f'Expected one occurrence: {old!r}'
    return text.replace(old, new, 1)


def index_text(path, text):
    blob = git('hash-object', '-w', '--stdin', data=text.encode('utf-8')).decode().strip()
    git('update-index', '--add', '--cacheinfo', '100644', blob, path)


if '--root-docs' in sys.argv:
    root = root.parent
    path = 'docs/architecture.md'
    text = replace_once(base(path), '例如音乐源、音频处理、音频解密、歌词和封面。',
        '例如音乐源、音频编解码、DSP、歌词和封面。NCM 与 FFmpeg 声明 `audio-codec`，声道转换声明 `audio-dsp`；分类不改变已有插件 ID、协议角色或业务能力。')
    text = replace_once(text, '独立声道转换插件位于 DSP 前，', '独立声道转换插件属于 DSP，位于效果链前，')
    paragraphs = [line for line in current(path).splitlines() if line.startswith('2026-10-22 音频编解码增量：')]
    assert len(paragraphs) == 1, 'Expected one codec architecture paragraph'
    text = replace_once(text, '| 播放描述字段 | 语义 |', paragraphs[0] + '\n\n| 播放描述字段 | 语义 |')
    index_text(path, text)
    print('Staged codec architecture without unrelated category edits.')
    sys.exit(0)


owned = '''
docs/contracts/plugins/v1/README.md
docs/contracts/plugins/v1/protocol.schema.json
docs/playback/audio-processing.md
docs/plugins/audio-codecs.md
docs/plugins/dsp-effects.md
docs/plugins/dsp-plugins.md
docs/plugins/plugin-identity.md
docs/plugins/plugin-sdk.md
plugins/decoder/ffmpeg/capabilities.go
plugins/decoder/ffmpeg/channel_logging.go
plugins/decoder/ffmpeg/channel_logging_test.go
plugins/decoder/ffmpeg/channel_reader.go
plugins/decoder/ffmpeg/codec.go
plugins/decoder/ffmpeg/codec_cleanup.go
plugins/decoder/ffmpeg/codec_cleanup_test.go
plugins/decoder/ffmpeg/codec_test.go
plugins/decoder/ffmpeg/codec_worker.go
plugins/decoder/ffmpeg/decode_output.go
plugins/decoder/ffmpeg/decode_pcm.go
plugins/decoder/ffmpeg/format.go
plugins/decoder/ffmpeg/format_test.go
plugins/decoder/ffmpeg/native_wave.go
plugins/decoder/ffmpeg/wav.go
plugins/decoder/ffmpeg/worker_version_test.go
plugins/decoder/ncm/chain.go
plugins/decoder/ncm/format.go
plugins/decoder/ncm/format_test.go
plugins/decoder/ncm/settings.go
scripts/prepare-codec-index.py
sdk/README.md
sdk/audioformat
sdk/bundledplugin/releases.go
src/app/api/timeline.go
src/app/bootstrap/audio_management.go
src/app/bootstrap/managed_probe.go
src/app/bootstrap/probe.go
src/app/bootstrap/server.go
src/library/scrape_decode.go
src/library/scrape_source.go
src/metadata/enrichmentworker/execute.go
src/metadata/identification/fingerprintworker/compute.go
src/playback/cache.go
src/playback/input.go
src/playback/plugin_lifecycle.go
src/playback/service.go
src/playback/stream.go
src/playback/wave.go
src/plugins/audiocodecs
src/plugins/codecsettings
src/plugins/pluginconfig/identity.go
src/plugins/plugincontrol/manager.go
src/plugins/sourceaccess/access.go
src/plugins/sourceaccess/scan.go
src/plugins/sourceaccess/stable.go
tests/contract/api/codec_output_test.go
tests/e2e/codec_settings_test.go
tests/integration/audiocodecs
tests/integration/playback/codec_settings_test.go
tests/integration/plugins/audio_test.go
tests/unit/pluginconfig/roles_test.go
tests/unit/plugincontrol/channelmix_test.go
tests/unit/plugins/format_recognition_test.go
'''.split()

# 所有部分暂存内容先计算完成，避免断言失败时留下半套索引。
partial = {}
path = 'plugins/decoder/ffmpeg/pipeline.go'
text = current(path)
text = replace_once(text, '\tBufferFrames int                `json:"bufferFrames,omitempty"`\n')
text = replace_once(text, '\tframes, err := bufferFrames(r.BufferFrames)\n\tif err != nil {\n\t\treturn err\n\t}\n')
text = replace_once(text, '\tif node.Output().Format != format {\n\t\treturn fmt.Errorf("UNSUPPORTED: DSP output differs from selected encoding layout")\n\t}\n')
text = replace_once(text, 'transferPCMFormatsWindow(ctx, converted, encodeInput, node, format, node.Output().Format, frames, window)',
                    'transferPCMFormatWindow(ctx, converted, encodeInput, node, format, window)')
partial[path] = text

path = 'plugins/decoder/ffmpeg/processor.go'
anchor = '\tif plan.TaskID == "" || plan.AssetID == "" || size <= 0 {'
partial[path] = replace_once(base(path), anchor,
    '\tif err := p.SupportsDecodeOutput(plan.DecodeOutput); err != nil {\n\t\treturn err\n\t}\n' + anchor)

path = 'plugins/decoder/ffmpeg/worker.go'
text = current(path)
text = replace_once(text, '\tif _, err := bufferFrames(r.BufferFrames); err != nil {\n\t\treturn nil, err\n\t}\n')
text = replace_once(text, '\t\toutput := node.Output()\n')
text = replace_once(text, '\t\tif output.Format != format {\n\t\t\treturn nil, fmt.Errorf("UNSUPPORTED: DSP output differs from selected encoding layout")\n\t\t}\n')
partial[path] = text

path = 'src/app/bootstrap/composition.go'
text = current(path)
text = replace_once(text, '\t"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"\n')
partial[path] = replace_once(text, '\thandler.RegisterDSPSettings(dspsettings.New(store, player.DSPSettingsConfig))\n')

path = 'src/playback/prepare.go'
text = replace_once(current(path), 'r CreateRequest, fixed ...*audioffmpeg.Processor)', 'r CreateRequest)')
partial[path] = replace_once(text, '''\tif len(fixed) > 0 {
\t\tp.processor = fixed[0]
\t} else {
\t\tp.processor, err = s.processorForUser(ctx, owner, p.processor)
\t\tif err != nil {
\t\t\tp.discard()
\t\t\treturn nil, err
\t\t}
\t}
''')

path = 'src/playback/recovery.go'
text = replace_once(current(path), '\t// 恢复沿用会话创建时的 DSP；用户编辑只影响新播放。\n')
text = replace_once(text, 's.recoveryProfileForSession(ctx, who.User.ID, a)', 's.RecoveryProfileForUser(ctx, who.User.ID)')
partial[path] = replace_once(text, 'GainDB: gain}}, a.processor)', 'GainDB: gain}})')

path = 'src/playback/timeline.go'
partial[path] = replace_once(current(path), '\tprocessor, err = s.processorForUser(ctx, owner, processor)\n\tif err != nil {\n\t\treturn timeline.Profile{}, err\n\t}\n')

path = 'docs/contracts/openapi.yaml'
anchor = '只接受插件声明的字段，不执行脚本、URL或任意宿主命令。'
partial[path] = replace_once(base(path), anchor, anchor + 'FFmpeg 的 decodeOutput 可选择 native、pcm_f32le、pcm_s16le，保存后对新播放会话生效，活动会话与指纹规格保持原有设置。')

# channelmix 的移动已由 git mv 暂存，保留索引中的旧实现，不带入工作树的 mono 扩展。
git('add', '--', *owned)
for path, text in partial.items():
    index_text(path, text)
print('Staged codec recognition, recursive decoding, transcoding, and plugin categories.')

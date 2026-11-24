"""局部更新全库批量关联协议，保留其他契约的格式。"""
import copy
import json
from pathlib import Path

root = Path(__file__).resolve().parents[1]


def replace_value(text, name, value):
    needle = json.dumps(name) + ':'
    start = text.index(needle) + len(needle)
    while text[start].isspace():
        start += 1
    _, length = json.JSONDecoder().raw_decode(text[start:])
    return text[:start] + json.dumps(value, ensure_ascii=False) + text[start + length:]


def set_member(text, container, name, value):
    if name in json.loads(text)[container]:
        return replace_value(text, name, value)
    needle = json.dumps(container) + ': {'
    entry = '\n    ' + json.dumps(name) + ': ' + json.dumps(value, ensure_ascii=False) + ','
    return text.replace(needle, needle + entry, 1)


schema_path = root / 'docs/contracts/http.schema.json'
schema = schema_path.read_text(encoding='utf-8')
definitions = json.loads(schema)['$defs']
request = {
    'type': 'object', 'additionalProperties': False,
    'required': ['expectedRevision', 'mode'],
    'properties': {
        'expectedRevision': {'type': 'integer', 'minimum': 0},
        'mode': {'enum': ['score', 'title_artist']},
        'minimumScore': {'type': 'integer', 'minimum': 0, 'maximum': 100}
    },
    'if': {'properties': {'mode': {'const': 'score'}}},
    'then': {'required': ['minimumScore']},
    'else': {'not': {'required': ['minimumScore']}}
}
result = copy.deepcopy(definitions['MatchingMutation'])
result['properties']['revision']['minimum'] = 0
result['properties']['changes']['minItems'] = 0
result['properties']['changes'].pop('maxItems', None)
for name in ['mergedGroups', 'mergedSources', 'skippedGroups']:
    result['required'].append(name)
    result['properties'][name] = {'type': 'integer', 'minimum': 0}
for name, value in [('BulkMergeRequest', request), ('BulkMergeResult', result)]:
    schema = set_member(schema, '$defs', name, value)
schema_path.write_text(schema, encoding='utf-8')

api_path = root / 'docs/contracts/openapi.yaml'
api = api_path.read_text(encoding='utf-8')
path = {'post': {
    'operationId': 'bulkMergeMatching',
    'description': '用户主动执行全库批量关联。score 只处理当前有效且达到 minimumScore 的候选；title_artist 直接按本人全部来源中当前可见且可用曲目的非空标题和完整作者集合匹配，不依赖候选或时长。保留已有录音全部成员，包括暂时不可用成员，忽略明确解除的来源；完整连通组含排除或超过256项时整组跳过。所有成功组在同一事务提交并共用一次人工修订；无匹配不推进修订。同幂等键重放包括统计的原结果。',
    'parameters': [{'$ref': '#/components/parameters/CSRF'}, {'$ref': '#/components/parameters/Idempotency'}],
    'requestBody': {'required': True, 'content': {'application/json': {'schema': {'$ref': './http.schema.json#/$defs/BulkMergeRequest'}}}},
    'responses': {
        '200': {'description': '批量关联结果及审计；计数包含扩展后的全部录音成员，跳过组不写入。', 'content': {'application/json': {'schema': {'$ref': './http.schema.json#/$defs/BulkMergeResult'}}}},
        'default': {'$ref': '#/components/responses/Error'}
    }
}}
api = set_member(api, 'paths', '/matching/bulk-merge', path)
api_path.write_text(api, encoding='utf-8')

fixtures_path = root / 'tests/testdata/contracts/api/matching.json'
fixtures = [item for item in json.loads(fixtures_path.read_text(encoding='utf-8'))
            if not item['name'].startswith('bulk-merge-')]
for name, valid, value in [
    ('score', True, {'expectedRevision': 0, 'mode': 'score', 'minimumScore': 0}),
    ('title-artist', True, {'expectedRevision': 0, 'mode': 'title_artist'}),
    ('score-missing-threshold', False, {'expectedRevision': 0, 'mode': 'score'}),
    ('score-high-threshold', False, {'expectedRevision': 0, 'mode': 'score', 'minimumScore': 101}),
    ('score-fraction', False, {'expectedRevision': 0, 'mode': 'score', 'minimumScore': 34.5}),
    ('title-extra-threshold', False, {'expectedRevision': 0, 'mode': 'title_artist', 'minimumScore': 35}),
    ('missing-revision', False, {'mode': 'title_artist'}),
    ('unknown-mode', False, {'expectedRevision': 0, 'mode': 'automatic'}),
    ('source-injection', False, {'expectedRevision': 0, 'mode': 'title_artist', 'sourceTrackIds': ['private']})
]:
    fixtures.append({'name': 'bulk-merge-' + name, 'schema': 'BulkMergeRequest', 'valid': valid, 'value': value})
fixtures_path.write_text(json.dumps(fixtures, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')

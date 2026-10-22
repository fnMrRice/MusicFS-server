"""局部更新重复识别与声明式设置契约，保留无关协议段落。"""
import json
from pathlib import Path

root = Path(__file__).resolve().parents[1]
schema_path = root / 'docs/contracts/http.schema.json'
api_path = root / 'docs/contracts/openapi.yaml'
schema = schema_path.read_text(encoding='utf-8')
api = api_path.read_text(encoding='utf-8')


def ref(name):
    return {'$ref': '#/$defs/' + name}


def obj(properties, required=None):
    return {'type': 'object', 'additionalProperties': False,
            'required': list(properties) if required is None else required, 'properties': properties}


def replace_definition(text, name, transform):
    needle = '"' + name + '":'
    start = text.index(needle) + len(needle)
    while text[start].isspace():
        start += 1
    value, length = json.JSONDecoder().raw_decode(text[start:])
    transform(value)
    return text[:start] + json.dumps(value, ensure_ascii=False) + text[start + length:]


text = {'type': 'string'}
localized = {'type': 'object', 'additionalProperties': text}
field = obj({'key': text, 'type': {'enum': ['text', 'number', 'checkbox', 'select']},
             'label': text, 'description': text, 'defaultValue': {},
             'min': {'type': 'number'}, 'max': {'type': 'number'},
             'localizedLabels': localized, 'localizedDescriptions': localized,
             'options': {'type': 'array', 'items': obj({'value': text, 'label': text,
                        'localizedLabels': localized}, ['value', 'label'])}},
            ['key', 'type', 'label', 'description', 'defaultValue'])
action = obj({'id': text, 'label': text, 'description': text,
              'localizedLabels': localized, 'localizedDescriptions': localized}, ['id', 'label', 'description'])
definitions = {
    'PluginSettingsField': field,
    'PluginSettingsSchema': obj({'revision': text, 'title': text, 'description': text,
                                'fields': {'type': 'array', 'items': ref('PluginSettingsField')},
                                'actions': {'type': 'array', 'items': action},
                                'localizedTitles': localized, 'localizedDescriptions': localized},
                               ['revision', 'title', 'description', 'fields', 'actions']),
    'PluginSettingsJob': obj({'jobId': {'anyOf': [ref('ID'), {'type': 'null'}]},
                             'status': {'enum': ['idle', 'queued', 'running', 'succeeded', 'failed']},
                             'processed': {'type': 'integer', 'minimum': 0},
                             'total': {'type': 'integer', 'minimum': 0},
                             'candidates': {'type': 'integer', 'minimum': 0},
                             'errorCode': {'type': ['string', 'null']},
                             'updatedAt': {'anyOf': [ref('Timestamp'), {'type': 'null'}]}}),
    'PluginSettingsRequest': obj({'expectedRevision': {'type': 'integer', 'minimum': 0},
                                 'schemaRevision': text, 'values': {'type': 'object'}}),
    'PluginSettingsPage': obj({'pluginId': text, 'schema': ref('PluginSettingsSchema'),
                              'revision': {'type': 'integer', 'minimum': 0},
                              'values': {'type': 'object'}, 'job': ref('PluginSettingsJob')})
}
for name, definition in definitions.items():
    if name in json.loads(schema)['$defs']:
        schema = replace_definition(schema, name, lambda value, d=definition: (value.clear(), value.update(d)))
    else:
        schema = schema.replace('"$defs": {', '"$defs": {\n    ' + json.dumps(name) + ': ' + json.dumps(definition, ensure_ascii=False) + ',', 1)

schema = replace_definition(schema, 'ManagedPlugin', lambda value: value['properties'].update({'settingsAvailable': {'type': 'boolean'}}))
schema = replace_definition(schema, 'SourceTrack', lambda value: value['properties'].update({
    'recordingId': {'anyOf': [ref('ID'), {'type': 'null'}]},
    'versions': {'type': 'array', 'minItems': 1, 'maxItems': 256, 'items': ref('SourceTrack')}}))
schema = replace_definition(schema, 'SourceTracks', lambda value: value['properties'].update({'matchingRevision': {'type': 'integer', 'minimum': 0}}))
schema = replace_definition(schema, 'MatchCandidate', lambda value: value['properties'].update({'leftTrack': ref('SourceTrack'), 'rightTrack': ref('SourceTrack')}))


def response(name):
    return {'description': name, 'content': {'application/json': {'schema': {'$ref': './http.schema.json#/$defs/' + name}}}}


def responses(name, status='200'):
    return {status: response(name), 'default': {'$ref': '#/components/responses/Error'}}


def body(name):
    return {'required': True, 'content': {'application/json': {'schema': {'$ref': './http.schema.json#/$defs/' + name}}}}


csrf = {'$ref': '#/components/parameters/CSRF'}
key = {'$ref': '#/components/parameters/Idempotency'}
paths = {
    '/matching/candidates': {'get': {'operationId': 'listLibraryMatchCandidates',
        'description': '分页列出本人全曲库当前待确认候选，附当前来源曲目信息；自动识别由插件实现，候选不会自动合并。',
        'parameters': [{'name': 'after', 'in': 'query', 'schema': {'type': 'string', 'maxLength': 256}},
                       {'name': 'limit', 'in': 'query', 'schema': {'type': 'integer', 'minimum': 1, 'maximum': 100, 'default': 20}}],
        'responses': responses('MatchCandidates')}},
    '/plugin-settings/{pluginId}': {
        'parameters': [{'name': 'pluginId', 'in': 'path', 'required': True, 'schema': text}],
        'get': {'operationId': 'getPluginSettings', 'responses': responses('PluginSettingsPage')},
        'put': {'operationId': 'updatePluginSettings', 'x-idempotency': 'natural', 'parameters': [csrf], 'requestBody': body('PluginSettingsRequest'),
                'description': '按schema与设置修订校验后替换当前用户偏好。写入仅在expectedRevision匹配时生效；原修订重试返回CONFLICT而不重复写入，响应不确定时先GET确认。只接受插件声明的字段，不执行脚本、URL或任意宿主命令。',
                'responses': responses('PluginSettingsPage')}},
    '/plugin-settings/{pluginId}/actions/{actionId}': {
        'parameters': [{'name': 'pluginId', 'in': 'path', 'required': True, 'schema': text},
                       {'name': 'actionId', 'in': 'path', 'required': True, 'schema': text}],
        'post': {'operationId': 'executePluginSettingsAction', 'parameters': [csrf, key],
                 'requestBody': {'required': True, 'content': {'application/json': {'schema': obj({})}}},
                 'description': '调用插件声明的动作。立即识别返回持久任务；同幂等键返回原任务，异键并发返回RATE_LIMITED。',
                 'responses': responses('PluginSettingsJob', '202')}}
}
for name, value in paths.items():
    if name in json.loads(api)['paths']:
        api = replace_definition(api, name, lambda old, new=value: (old.clear(), old.update(new)))
    else:
        api = api.replace('"paths": {', '"paths": {\n    ' + json.dumps(name) + ': ' + json.dumps(value, ensure_ascii=False) + ',', 1)


def group_query(value):
    params = value['get']['parameters']
    if not any(p.get('name') == 'groupBy' for p in params):
        params.append({'name': 'groupBy', 'in': 'query', 'schema': {'enum': ['recording']}})
    value['get']['description'] = '普通音乐范围排除本人已发布ASMR作品或CD抓轨副本的音频，分类及计数只含可见成员。groupBy=recording时先按本人录音关联聚合再分页；含可见音乐versions及matchingRevision，优先可播放版本为代表。provider过滤保留组内所有可见音乐来源。游标绑定本人、provider和分组模式，固定两种修订，10分钟过期。'


api = replace_definition(api, '/source-tracks', group_query)
json.loads(schema)
json.loads(api)
schema_path.write_text(schema, encoding='utf-8')
api_path.write_text(api, encoding='utf-8')

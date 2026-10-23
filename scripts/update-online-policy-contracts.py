"""只更新在线契约定义，保留其它模块的并行修改。"""
from pathlib import Path
import json
import re

ROOT = Path(__file__).resolve().parents[1]
# 新增必需声明后拒绝旧修订 Worker，避免握手成功却没有资料策略。
for name in [
    'sdk/pluginproto/online.go', 'docs/providers/platform-login.md',
    'docs/plugins/plugin-sdk.md', 'docs/plugins/plugin-identity.md',
    'docs/contracts/plugins/v1/README.md', 'docs/contracts/plugins/v1/account.md',
    'docs/contracts/plugins/v1/online.schema.json', 'docs/contracts/http.schema.json',
    'tests/testdata/contracts/api/samples.json', 'tests/testdata/contracts/plugins/samples.json',
]:
    path = ROOT/name
    text = path.read_text(encoding='utf-8')
    path.write_text(text.replace('online-authenticated-v3', 'online-declared-v4'), encoding='utf-8', newline='\n')
IDENTITY = r'^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$'
PLATFORM = r'^[a-z][a-z0-9-]{0,63}$'
URL = r'^https://[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?\.[a-z]{2,63}/[^#\s]*$'
IMAGE = URL.replace('[^#', '[^?#')

def obj(properties, required=None):
    return {'type':'object','additionalProperties':False,'required':list(properties) if required is None else required,'properties':properties}

reference_rule = obj({'idPattern':{'type':'string','minLength':2,'maxLength':512}, 'urlTemplate':{'type':'string','maxLength':2048,'pattern':r'^https://'}})
resources = obj({**{key:{'type':['array','null'],'maxItems':16,'items':{'$ref':'#/$defs/OnlineReferenceRule'}} for key in ['tracks','playlists','albums','artists']},'playlistEntryPatterns':{'type':['array','null'],'maxItems':16,'items':{'type':'string','maxLength':512}},'imageHosts':{'type':['array','null'],'maxItems':16,'items':{'type':'string','maxLength':253}}})
login = obj({'method':{'type':'string','pattern':r'^[a-zA-Z][a-zA-Z0-9.-]{0,63}$'},'validation':{'type':'string','minLength':1,'maxLength':4096}})
options = obj({'method':{'type':'string','minLength':1,'maxLength':64},'developerToken':{'type':'string','maxLength':16384},'configured':{'type':'boolean'},'validation':{'type':'string','minLength':1,'maxLength':4096}}, ['method','configured','validation'])

def rewrite_refs(value, prefix):
    if isinstance(value,dict):
        return {k:(v.replace('#/$defs/Online','#/$defs/'+prefix) if k=='$ref' else rewrite_refs(v,prefix)) for k,v in value.items()}
    if isinstance(value,list): return [rewrite_refs(v,prefix) for v in value]
    return value

def generic(value):
    if isinstance(value,list):
        for item in value: generic(item)
    if not isinstance(value,dict): return
    for key,item in list(value.items()):
        if key=='properties':
            for name,schema in item.items():
                if name in ['externalId','externalTrackId']:
                    schema.clear(); schema.update(type='string',pattern=IDENTITY)
                elif name=='externalIds' and 'items' in schema:
                    schema['items']={'type':'string','pattern':IDENTITY}
                elif name=='sourceUrl':
                    schema.clear(); schema.update(type='string',maxLength=2048,pattern=URL)
                elif name in ['coverUrl','imageUrl']:
                    for child in schema.get('anyOf',[]):
                        if child.get('type')=='string': child.update(pattern=IMAGE)
                elif name=='platform':
                    schema.clear(); schema.update(type='string',pattern=PLATFORM)
                elif name=='pluginId' and 'enum' in schema:
                    schema.clear(); schema.update(type='string',minLength=1,maxLength=128)
                elif name=='nextCursor':
                    for child in schema.get('anyOf',[]):
                        if child.get('type')=='string': child['pattern']=r'^\S+$'
        generic(item)

def replace_def(text,key,value):
    match = re.search(r'^\s{4}"'+re.escape(key)+r'"\s*:\s*',text,re.M)
    if match:
        _, size = json.JSONDecoder().raw_decode(text[match.end():])
        return text[:match.end()]+json.dumps(value,ensure_ascii=False,separators=(',',':'))+text[match.end()+size:]
    return text.replace('"$defs": {','"$defs": {\n    '+json.dumps(key)+': '+json.dumps(value,ensure_ascii=False,separators=(',',':'))+',',1)

for name,prefix in [('docs/contracts/plugins/v1/online.schema.json',''),('docs/contracts/http.schema.json','Online')]:
    path=ROOT/name
    text=path.read_text(encoding='utf-8')
    parsed=json.loads(text)
    for key,value in parsed['$defs'].items():
        if prefix and not (key.startswith('Online') or key in ['ImportRemotePlaylistRequest','RemotePlaylist','RemotePlaylistBinding','RemoteSourceRef','RemoteSource','RecommendationSource']): continue
        generic(value)
        if key in ['TrackLookup','OnlineTrackLookup','PlaylistTrack','OnlinePlaylistTrack']:
            value.pop('allOf',None)
        if key=='ImportRemotePlaylistRequest':
            value.pop('allOf',None)
            value['properties']['accountId']['description']='账号要求取决于实例认证声明；匿名来源使用空值，账号来源由宿主核对当前账号。'
        if key in ['Descriptor','OnlineDescriptor','OnlineInstance']:
            value.pop('allOf',None)
            value['properties']['resources']={'$ref':'#/$defs/'+prefix+'ResourcePolicy'}
            value['properties']['login']={'$ref':'#/$defs/'+prefix+'LoginDescriptor'}
            if 'resources' not in value['required']: value['required'].append('resources')
            value['properties']['configSchema']={'type':'object'}
            value['properties']['configDefaults']={'type':'object'}
        if key=='Manifest':
            value.pop('allOf',None)
            value['properties']['capabilities']['maxItems']=18
            value['properties']['capabilities']['items']['enum']=parsed['$defs']['Capability']['properties']['name']['enum']
        if key=='Request':
            base=json.loads(json.dumps(value['oneOf'][1]))
            base['properties']['method']={'const':'provider.online.accountOptions'}
            if base not in value['oneOf']: value['oneOf'].append(base)
        if key=='Success':
            result=value['properties']['result']['oneOf']
            reference={'$ref':'#/$defs/AccountOptionsResult'}
            if reference not in result: result.append(reference)
        text=replace_def(text,key,value)
    for key,value in [('ReferenceRule',reference_rule),('ResourcePolicy',resources),('LoginDescriptor',login),('AccountOptions',options)]:
        text=replace_def(text,prefix+key,rewrite_refs(value,prefix))
    if not prefix:
        text=replace_def(text,'AccountOptionsResult',obj({'context':{'$ref':'#/$defs/Context'},'value':{'$ref':'#/$defs/AccountOptions'}}))
    path.write_text(text,encoding='utf-8',newline='\n')

path=ROOT/'docs/contracts/openapi.yaml'
text=path.read_text(encoding='utf-8')
for key,value in json.loads(text)['paths'].items():
    if not key.startswith('/online-instances'): continue
    generic(value)
    for node in value.get('parameters',[]):
        if node.get('name')=='externalId': node['schema']={'type':'string','pattern':IDENTITY}
    text=replace_def(text,key,value)
path.write_text(text,encoding='utf-8',newline='\n')

# 静态样例显式保留空资料规则，真实插件样例在测试中从声明生成。
for name in ['tests/testdata/contracts/api/samples.json','tests/testdata/contracts/plugins/samples.json']:
    path=ROOT/name
    value=json.loads(path.read_text(encoding='utf-8'))
    def visit(item):
        if isinstance(item,dict):
            if item.get('schemaRevision')=='online-declared-v4':
                item['resources']={key:None for key in resources['properties']}
            for child in item.values(): visit(child)
        elif isinstance(item,list):
            for child in item: visit(child)
    visit(value)
    # 保留每个样例一行的原有格式，避免无关格式差异。
    path.write_text('[\n'+',\n'.join('  '+json.dumps(item,ensure_ascii=False,separators=(',',':')) for item in value)+'\n]\n',encoding='utf-8',newline='\n')

path=ROOT/'docs/contracts/platform-account.schema.json'
text=path.read_text(encoding='utf-8')
data=json.loads(text)
view=data['$defs']['View']
view['properties']['method']=login['properties']['method']
view['properties']['avatarUrl']={'type':'string','maxLength':2048,'pattern':IMAGE}
view['properties']['qrUrl']={'type':'string','maxLength':32768,'pattern':URL}
text=replace_def(text,'View',view)
text=replace_def(text,'Options',options)
path.write_text(text,encoding='utf-8',newline='\n')

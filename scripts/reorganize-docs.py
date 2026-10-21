"""保留文档分类与合并映射；显式 --apply 后执行，可重复运行。"""

from pathlib import Path
import json, os, re, subprocess, sys, unicodedata
from urllib.parse import unquote

root = Path(__file__).resolve().parents[2]

groups = {
"operations": ["running","deployment","backup","lifecycle","configuration"],
"development": ["dependencies"],
"plugins": ["plugin-architecture","plugin-sdk","plugin-identity","plugin-configuration","dsp-plugins","storage-plugins"],
"library": ["albums","catalog-query","personal-library","playlists","source-playlists","remote-playlists","recommendations","scanning"],
"metadata": ["identification","matching","metadata","media","writeback"],
"playback": ["audio-processing","processed-cache","recovery","ncm"],
"providers": ["online","platform-login"],
"design": ["nas-providers","bilibili-provider"],
}
moves = {"server/docs/"+name+".md":"server/docs/"+group+"/"+name+".md" for group,names in groups.items() for name in names}
moves.update({
"server/docs/development.md":"server/docs/development/README.md",
"server/docs/implementation/plugin-management.md":"server/docs/plugins/plugin-management.md",
"server/docs/implementation/nas-bilibili.md":"server/docs/design/nas-bilibili.md",
"server/docs/web-ui-api-gaps.md":"server/docs/contracts/web-integration.md",
})
merges = {
"server/docs/implementation.md":("server/docs/README.md",""),
"server/docs/implementation/00-project-layout.md":("server/docs/development.md",""),
"server/docs/implementation/01-foundation.md":("server/docs/architecture.md",""),
"server/docs/implementation/02-library-and-scan.md":("server/docs/scanning.md",""),
"server/docs/implementation/03-playback.md":("server/docs/audio-processing.md",""),
"server/docs/implementation/04-audio-processing.md":("server/docs/audio-processing.md",""),
"server/docs/implementation/05-identification.md":("server/docs/identification.md",""),
"server/docs/implementation/06-metadata.md":("server/docs/metadata.md",""),
"server/docs/implementation/07-online-library.md":("server/docs/online.md",""),
"server/docs/implementation/08-release.md":("server/docs/deployment.md",""),
"server/docs/implementation/plugins.md":("server/docs/plugin-architecture.md",""),
"server/docs/fingerprinting.md":("server/docs/identification.md","本地音频指纹"),
"server/docs/media-storage.md":("server/docs/media.md","发布回收与备份"),
"server/docs/derived-media.md":("server/docs/media.md","派生内容"),
"server/docs/platform-capabilities.md":("server/docs/platform-login.md",""),
"server/docs/provider-references.md":("server/docs/implementation/nas-bilibili.md","候选依赖"),
"server/docs/validation/resource-baseline.md":("server/docs/validation/verification.md",""),
}
def resolve(path,fragment=""):
    rel=path.replace("\\","/")
    if rel.startswith("server/docs/validation/") and rel.endswith(".md") and rel.rsplit("/",1)[1] not in ["README.md","verification.md","fixtures.md"]:
        rel,fragment=merges.get(rel,("server/docs/validation/README.md",""))
    if rel in merges:
        rel,fragment=merges[rel]
    return moves.get(rel,rel),fragment

if '--apply' not in sys.argv:
    print('Use --apply to organize documentation and update links in the workspace.')
    sys.exit(0)

paths = [Path(p) for p in subprocess.check_output(["rg","--files","-g","*.md"],cwd=root,text=True,encoding="utf-8").splitlines()]
sources = {p.as_posix():(root/p).read_text(encoding="utf-8-sig") for p in paths}
for old,new in moves.items():
    if not (root/old).exists():
        continue
    (root/new).parent.mkdir(parents=True,exist_ok=True)
    subprocess.run(["git","mv",old.removeprefix("server/"),new.removeprefix("server/")],cwd=root/"server",check=True,capture_output=True)
def anchors(text):
    out=set()
    dup={}
    clean=re.sub(r"\x60{3}.*?\x60{3}","",text,flags=re.S)
    for h in re.findall(r"^#{1,6}\s+(.+?)\s*#*\s*$",clean,re.M):
        h=re.sub(r"\[([^\]]+)\]\([^)]+\)",r"\1",h).replace("\x60","")
        h=re.sub("<[^>]+>","",h).lower()
        h="".join(c for c in h if c in "_- " or not unicodedata.category(c).startswith(("P","S")))
        h=h.replace(" ","-")
        n=dup.get(h,0);dup[h]=n+1
        out.add(h+("-"+str(n) if n else ""))
    out.update(re.findall(r'<a\s+(?:id|name)=["\x27]([^"\x27]+)',text))
    return out
anch={moves.get(p,p):anchors(t) for p,t in sources.items()}
changed=[]
pattern=re.compile(r"(!?\[[^\]\n]*\]\()([^)\n]+)(\))")
for old,text in sources.items():
    new=moves.get(old,old)
    def rewrite(m):
        target=m.group(2)
        if target.startswith(("http:","https:","mailto:","data:","plugin:","app:","codex:")):return m.group(0)
        # 当前仓库链接不使用可选标题或含右括号的编码路径。
        if " " in target and not target.startswith("<"):return m.group(0)
        target=target.strip("<>")
        path,sep,frag=target.partition("#")
        if not path:
            absrel=old
        else:
            absrel=os.path.relpath(os.path.normpath(root/Path(old).parent/unquote(path)),root).replace("\\","/")
        mapped,newfrag=resolve(absrel,unquote(frag))
        if mapped.startswith("server/") and newfrag and mapped in anch and newfrag not in anch[mapped]:
            newfrag=""
        if mapped==absrel and new==old and newfrag==frag:return m.group(0)
        outpath=os.path.relpath(root/mapped,(root/new).parent).replace("\\","/") if path or mapped!=new else ""
        dest=outpath+("#"+newfrag if newfrag else "")
        label=m.group(1)
        if mapped=="server/docs/validation/README.md" and absrel!=mapped:
            label="[待验收清单]("
        elif mapped=="server/docs/validation/verification.md" and absrel!=mapped:
            label="[复验方法]("
        elif absrel=="server/docs/implementation.md":
            label="[文档导航]("
        elif absrel=="server/docs/implementation/00-project-layout.md":
            label="[开发与检查]("
        return label+dest+m.group(3)
    updated=pattern.sub(rewrite,text)
    # 仅更新明确指向服务端文档的字面路径。
    for src,dst in sorted(moves.items(), key=lambda x:-len(x[0])):
        updated=updated.replace("\x60"+src+"\x60","\x60"+dst+"\x60")
        if new.startswith("server/"):
            updated=updated.replace("\x60"+src.removeprefix("server/")+"\x60","\x60"+dst.removeprefix("server/")+"\x60")
    if updated!=text:
        (root/new).write_text(updated,encoding="utf-8",newline="\n")
        changed.append(new)
print(json.dumps({"moved":len(moves),"links_updated_in":len(changed),"external_files":[p for p in changed if not p.startswith("server/")]},ensure_ascii=False))

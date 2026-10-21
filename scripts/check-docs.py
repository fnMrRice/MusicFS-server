"""检查服务端文档、导航和跨仓库入站链接；不访问网络。"""

from pathlib import Path
import re
import subprocess
import sys
import unicodedata
from urllib.parse import unquote


SERVER = Path(__file__).resolve().parents[1]
WORKSPACE = SERVER.parent
LINK = re.compile(r"!?\[[^\]\n]*\]\(([^)\n]+)\)")


def headings(text):
    """按 GitHub 的常用标题形式生成章节锚点。"""
    anchors, counts = set(), {}
    text = re.sub(r"```.*?```|~~~.*?~~~", "", text, flags=re.S)
    for title in re.findall(r"^#{1,6}\s+(.+?)\s*#*\s*$", text, re.M):
        title = re.sub(r"\[([^\]]+)\]\([^)]+\)", r"\1", title)
        title = re.sub(r"<[^>]+>", "", title).replace("`", "").lower()
        slug = "".join(
            char for char in title
            if char in "_- " or not unicodedata.category(char).startswith(("P", "S"))
        ).replace(" ", "-")
        count = counts.get(slug, 0)
        counts[slug] = count + 1
        anchors.add(slug + (f"-{count}" if count else ""))
    anchors.update(re.findall(r'<a\s+(?:id|name)=["\x27]([^"\x27]+)', text))
    return anchors


def main():
    result = subprocess.run(
        ["rg", "--files", "-g", "*.md"], cwd=WORKSPACE,
        capture_output=True, text=True, encoding="utf-8", check=True,
    )
    files = [WORKSPACE / name for name in result.stdout.splitlines()]
    contents = {path.resolve(): path.read_text(encoding="utf-8-sig") for path in files}
    anchors = {path: headings(text) for path, text in contents.items()}
    errors, checked, incoming = [], 0, 0
    navigation = set()
    for source, text in contents.items():
        # 代码块内的示例不作为 Markdown 链接。
        text = re.sub(r"```.*?```|~~~.*?~~~", "", text, flags=re.S)
        for match in LINK.finditer(text):
            raw = match.group(1)
            if re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", raw):
                continue
            raw = re.sub(r'\s+"[^"]*"$', "", raw).strip("<>")
            name, _, fragment = raw.partition("#")
            target = (source.parent / unquote(name)).resolve() if name else source
            is_server = source.is_relative_to(SERVER)
            if not is_server and not target.is_relative_to(SERVER):
                continue
            checked += 1
            incoming += not is_server
            label = str(source.relative_to(WORKSPACE)) + " -> " + raw
            if not target.exists():
                errors.append("Missing target: " + label)
            elif target.suffix == ".md" and fragment:
                if unquote(fragment) not in anchors.get(target, set()):
                    errors.append("Missing anchor: " + label)
            if source == SERVER / "docs" / "README.md":
                navigation.add(target)
    for path in contents:
        if path.is_relative_to(SERVER / "docs") and path != SERVER / "docs" / "README.md":
            if path not in navigation:
                errors.append("Missing navigation entry: " + str(path.relative_to(SERVER)))
    print(f"Checked {checked} local links ({incoming} incoming); {len(errors)} errors.")
    for error in errors:
        print(error)
    return bool(errors)


if __name__ == "__main__":
    sys.exit(main())

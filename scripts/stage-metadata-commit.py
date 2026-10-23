"""只暂存 NCM/元数据刷新在共享文件中的改动，不修改工作区正文。"""

import re
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def git(*args, data=None):
    return subprocess.check_output(["git", *args], cwd=ROOT, input=data)


def head(path):
    return git("show", f"HEAD:{path}").decode("utf-8")


def working(path):
    return (ROOT / path).read_text(encoding="utf-8")


def stage(path, content):
    mode = git("ls-tree", "HEAD", "--", path).decode().split()[0]
    digest = git("hash-object", "-w", "--stdin", data=content.encode()).decode().strip()
    git("update-index", "--cacheinfo", f"{mode},{digest},{path}")
    print(f"Staged metadata changes: {path}")


def main():
    path = "src/app/api/library.go"
    base, current = head(path), working(path)
    request = r"\t\tvar request struct \{\n.*?\n\t\t\}"
    before = re.search(request, base, re.S)
    after = re.search(request, current, re.S)
    assert before and after and "*pluginproto.ScanOverrides" in after[0]
    result = base.replace(before[0], after[0], 1)
    old_line = next(line for line in base.splitlines() if "input := library.ScanInput" in line)
    new_line = next(line for line in current.splitlines() if "input := library.ScanInput" in line)
    assert "RefreshMetadata: request.Options.RefreshMetadata" in new_line
    result = result.replace(old_line, new_line, 1)
    stage(path, result)

    path = "docs/contracts/http.schema.json"
    current = working(path)
    excluded = ("ScanStatus", "RootStatistic", "RootStatistics")
    result = "".join(line for line in current.splitlines(keepends=True)
                     if not any(line.startswith(f'    "{key}":') for key in excluded))
    assert '"ScanOverrides":' in result and '"refreshMetadata"' in result
    stage(path, result)

    path = "docs/contracts/openapi.yaml"
    base, current = head(path), working(path)
    result = base
    for marker in ("逐来源查看成功扫描所触发", '"operationId": "createScan"'):
        old_line = next(line for line in base.splitlines() if marker in line)
        new_line = next(line for line in current.splitlines() if marker in line)
        assert "refreshMetadata" in new_line
        result = result.replace(old_line, new_line, 1)
    stage(path, result)


if __name__ == "__main__":
    main()

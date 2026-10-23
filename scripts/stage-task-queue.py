"""从已验收快照精确暂存任务队列，保留并行开发的工作区内容。"""

from __future__ import annotations

import argparse
import json
import runpy
import subprocess
from pathlib import Path


SERVER = Path(__file__).resolve().parents[1]
BASELINE = "668910dae3d7871faa63f1269b866c01774c3dcb"
TASK_FILES = runpy.run_path(str(SERVER / "scripts/validate-task-snapshot.py"))["FILES"]
DOCUMENTS = (
    "docs/library/tasks.md",
    "scripts/validate-task-snapshot.py", "scripts/stage-task-queue.py",
)
REMOVED = (
    "plugins/database/sqlite/migrations/028_batch_011_028.sql",
    "plugins/database/sqlite/migrations/029_artwork_import.sql",
)


def git(*args: str, data: bytes | None = None) -> bytes:
    return subprocess.run(["git", *args], cwd=SERVER, input=data, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, check=True).stdout


def text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def contract_block(snapshot: Path, path: str, first: str, after: str) -> bytes:
    baseline = git("show", f"HEAD:{path}").decode("utf-8")
    current = text(SERVER / path)
    if current.count(first) != 1 or current.count(after) != 1 or baseline.count(after) != 1:
        raise RuntimeError(f"Unexpected contract anchors: {path}")
    start, end = current.index(first), current.index(after)
    if start >= end or first in baseline:
        raise RuntimeError(f"Unexpected task contract block: {path}")
    result = baseline.replace(after, current[start:end] + after, 1)
    if json.loads(result) != json.loads(text(snapshot / path)):
        raise RuntimeError(f"Task contract differs from validated snapshot: {path}")
    return result.encode("utf-8")


def task_document(path: str, inserts: tuple[tuple[str, str, str], ...]) -> bytes:
    baseline = git("show", f"HEAD:{path}").decode("utf-8")
    lines = text(SERVER / path).splitlines()
    for prefix, anchor, suffix in inserts:
        matches = [line for line in lines if line.startswith(prefix)]
        if len(matches) != 1 or baseline.count(anchor) != 1:
            raise RuntimeError(f"Unexpected task documentation anchor: {path}")
        baseline = baseline.replace(anchor, matches[0] + suffix + anchor, 1)
    return baseline.encode("utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("snapshot", type=Path)
    snapshot = parser.parse_args().snapshot.resolve()
    if not snapshot.is_relative_to((SERVER / "build").resolve()):
        raise RuntimeError("Snapshot must be inside the server build directory")
    if git("rev-parse", "HEAD").decode().strip() != BASELINE:
        raise RuntimeError("Repository HEAD differs from the validated baseline")

    entries: dict[str, bytes] = {}
    for path in TASK_FILES:
        current = text(SERVER / path)
        if current != text(snapshot / path):
            raise RuntimeError(f"Task source differs from validated snapshot: {path}")
        entries[path] = current.encode("utf-8")
    for path in DOCUMENTS:
        entries[path] = text(SERVER / path).encode("utf-8")

    entries["docs/README.md"] = task_document("docs/README.md", (
        ("| [目录刷新任务]", "| [曲库查询]", "\n"),))
    entries["docs/contracts/web-integration.md"] = task_document("docs/contracts/web-integration.md", (
        ("| 目录刷新任务队列 |", "| 管理员日志等级 |", "\n"),
        ("- 全部刷新按最新目录列表", "- `availableOnly`", "\n")))
    entries["docs/library/scanning.md"] = task_document("docs/library/scanning.md", (
        ("手动目录元数据刷新使用同一创建接口", "每个逻辑扫描任务最多执行三次。", "\n\n"),))
    path = "docs/contracts/README.md"
    baseline = git("show", f"HEAD:{path}").decode("utf-8")
    anchor = "[扫描范围](../library/scanning.md)、"
    if baseline.count(anchor) != 1:
        raise RuntimeError("Unexpected contracts documentation anchor")
    entries[path] = baseline.replace(anchor, anchor + "[目录刷新任务](../library/tasks.md)、", 1).encode("utf-8")

    # 架构文档也由并行功能维护，只插入本轮任务模块段落。
    path = "docs/architecture.md"
    baseline = git("show", f"HEAD:{path}").decode("utf-8")
    paragraphs = [line for line in text(SERVER / path).splitlines() if line.startswith("`src/tasks` 提供[")]
    anchor = "在线业务能力与平台授权分别检查；"
    if len(paragraphs) != 1 or baseline.count(anchor) != 1:
        raise RuntimeError("Unexpected architecture task paragraph")
    entries[path] = baseline.replace(anchor, paragraphs[0] + "\n\n" + anchor, 1).encode("utf-8")

    # 接入文件和 RPC 使用隔离快照版本，不向工作区写入或回退任何内容。
    for path in ("src/app/bootstrap/composition.go", "src/storage/backend.go"):
        entries[path] = text(snapshot / path).encode("utf-8")
    for path in sorted((snapshot / "src/storage/rpcbackend").glob("client_*.go")):
        entries[path.relative_to(snapshot).as_posix()] = text(path).encode("utf-8")
    entries["docs/contracts/http.schema.json"] = contract_block(
        snapshot, "docs/contracts/http.schema.json", '    "TaskRoot": {', '    "ScanRequest": {')
    entries["docs/contracts/openapi.yaml"] = contract_block(
        snapshot, "docs/contracts/openapi.yaml", '    "/tasks": {', '    "/scan-jobs/{id}": {')

    allowed = set(entries) | set(REMOVED)
    staged = set(git("diff", "--cached", "--name-only", "-z").decode().rstrip("\0").split("\0")) - {""}
    if staged - allowed:
        raise RuntimeError(f"Unexpected pre-existing staged paths: {sorted(staged - allowed)}")
    # 所有验证先完成，再写对象和索引；hash-object 按真实路径执行换行过滤。
    for path, content in entries.items():
        mode_line = git("ls-tree", "HEAD", "--", path).decode()
        mode = mode_line.split()[0] if mode_line else "100644"
        digest = git("hash-object", "-w", f"--path={path}", "--stdin", data=content).decode().strip()
        git("update-index", "--add", "--cacheinfo", f"{mode},{digest},{path}")
    for path in REMOVED:
        git("update-index", "--force-remove", "--", path)
    print(git("diff", "--cached", "--stat").decode(), end="")
    print(f"Staged task queue against {BASELINE}; working files were not changed.")


if __name__ == "__main__":
    main()

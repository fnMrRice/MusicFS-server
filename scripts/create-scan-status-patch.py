"""根据保存的原文生成本轮改动，避免混入工作区内其他任务的编辑。"""

import argparse
import difflib
import json
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--baseline", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    manifest = json.loads((args.baseline / "manifest.json").read_text(encoding="utf-8-sig"))
    chunks = []
    changed = []
    for entry in sorted(manifest, key=lambda item: item["path"]):
        relative = entry["path"]
        previous = args.baseline / relative
        current = args.source / relative
        before = previous.read_text(encoding="utf-8-sig") if entry["exists"] else ""
        after = current.read_text(encoding="utf-8-sig") if current.exists() else ""
        if before == after:
            continue
        changed.append(relative)
        chunks.append(f"diff --git a/{relative} b/{relative}\n")
        if not entry["exists"]:
            chunks.append("new file mode 100644\n")
        if not current.exists():
            chunks.append("deleted file mode 100644\n")
        chunks.extend(difflib.unified_diff(
            before.splitlines(keepends=True), after.splitlines(keepends=True),
            fromfile=f"a/{relative}" if entry["exists"] else "/dev/null",
            tofile=f"b/{relative}" if current.exists() else "/dev/null",
        ))
    args.output.write_text("".join(chunks), encoding="utf-8", newline="\n")
    print(json.dumps({"patch": str(args.output), "files": changed}, ensure_ascii=False))


if __name__ == "__main__":
    main()

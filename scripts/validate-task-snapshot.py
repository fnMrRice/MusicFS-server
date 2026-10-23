"""将任务队列改动复制到独立基线快照，避免并行开发影响定向验收。"""

from __future__ import annotations

import json
import shutil
import subprocess
import tarfile
import tempfile
from pathlib import Path


SERVER = Path(__file__).resolve().parents[1]
FILES = (
    "plugins/database/sqlite/automation_active.go",
    "plugins/database/sqlite/automation_outbox.go",
    "plugins/database/sqlite/enrichment_apply.go",
    "plugins/database/sqlite/enrichment_queue.go",
    "plugins/database/sqlite/refresh_tasks.go",
    "plugins/database/sqlite/refresh_task_summary.go",
    "plugins/database/sqlite/refresh_task_cancel.go",
    "plugins/database/sqlite/scan_job_recovery.go",
    "plugins/database/sqlite/migrations/029_batch_011_029.sql",
    "plugins/database/sqlite/migrations/033_refresh_task_cancellation.sql",
    "plugins/database/sqlite/migrations/README.md",
    "src/app/api/tasks.go",
    "src/library/jobs/cancel.go",
    "src/library/jobs/service.go",
    "src/library/task_names.go",
    "src/metadata/enrichment/automatic_models.go",
    "src/metadata/enrichmentworker/automatic.go",
    "src/metadata/enrichmentworker/automatic_apply.go",
    "src/metadata/enrichmentworker/execute.go",
    "src/metadata/enrichmentworker/interrupt.go",
    "src/metadata/enrichmentworker/service.go",
    "src/tasks/models.go",
    "src/tasks/service.go",
    "tests/contract/api/tasks_test.go",
    "tests/contract/api/tasks_storage_test.go",
    "tests/integration/enrichment/refresh_cancel_test.go",
    "tests/integration/library/task_cancel_test.go",
    "tests/integration/storage/refresh_tasks_test.go",
    "tests/integration/storage/refresh_task_migration_test.go",
    "tests/integration/storage/refresh_task_rpc_test.go",
    "tests/testdata/contracts/api/tasks.json",
    "tests/unit/jobs/cancel_test.go",
    "tests/unit/tasks/service_test.go",
)


def replace_once(path: Path, old: str, new: str) -> None:
    text = path.read_text(encoding="utf-8")
    if text.count(old) != 1:
        raise RuntimeError(f"Expected one snapshot anchor in {path}: {old!r}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8", newline="\n")


def merge_contract(snapshot: Path, relative: str, section: str, names: tuple[str, ...]) -> None:
    target = snapshot / relative
    base = json.loads(target.read_text(encoding="utf-8"))
    current = json.loads((SERVER / relative).read_text(encoding="utf-8"))
    for name in names:
        if name in base[section]:
            raise RuntimeError(f"Snapshot baseline already contains {name}")
        base[section][name] = current[section][name]
    target.write_text(json.dumps(base, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    build = SERVER / "build"
    build.mkdir(exist_ok=True)
    snapshot = Path(tempfile.mkdtemp(prefix="task-validation-", dir=build)).resolve()
    # 快照位于忽略的 build 下，保留日志用于复核；不触碰工作区和共享暂存区。
    archive = snapshot / "baseline.tar"
    subprocess.run(["git", "archive", "--format=tar", f"--output={archive}", "HEAD"], cwd=SERVER, check=True)
    with tarfile.open(archive) as content:
        content.extractall(snapshot, filter="data")
    archive.unlink()
    for relative in FILES:
        destination = snapshot / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(SERVER / relative, destination)
    for relative in (
        "plugins/database/sqlite/migrations/028_batch_011_028.sql",
        "plugins/database/sqlite/migrations/029_artwork_import.sql",
    ):
        (snapshot / relative).unlink()

    # 两个共享文件只应用任务接入，不复制并行在线音频实现。
    composition = snapshot / "src/app/bootstrap/composition.go"
    replace_once(composition, '\t_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"',
                 '\t_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"\n\t"github.com/fnMrRice/MusicFS-server/src/tasks"')
    replace_once(composition, "\thandler.RegisterEnrichment(metadata)",
                 "\thandler.RegisterEnrichment(metadata)\n\ttaskService := tasks.New(store, queue, metadata, lib.TaskNames)\n\tbeforeTasks := cleanup\n\tcleanup = func() { taskService.Close(); beforeTasks() }\n\thandler.RegisterTasks(taskService)")
    backend = snapshot / "src/storage/backend.go"
    replace_once(backend, '\t"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"',
                 '\t"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"\n\t"github.com/fnMrRice/MusicFS-server/src/tasks"')
    replace_once(backend, "\tjobs.Repository", "\tjobs.Repository\n\ttasks.Repository")
    merge_contract(snapshot, "docs/contracts/http.schema.json", "$defs",
                   ("Task", "TaskRoot", "TaskProgress", "TaskResult", "TaskPage", "TaskCancelRequest"))
    merge_contract(snapshot, "docs/contracts/openapi.yaml", "paths",
                   ("/tasks", "/tasks/{id}", "/tasks/{id}/cancel"))

    # 在隔离接口上重新生成持久化 RPC，避免带入其它功能新增的方法。
    print(f"Snapshot: {snapshot}", flush=True)
    subprocess.run(["go", "run", "./scripts/generate-storage-rpc"], cwd=snapshot, check=True)
    log = snapshot / "task-validation.log"
    command = ["go", "test", "./tests/contract/api", "./tests/integration/storage",
               "-run", "Test(TaskQueue|AllModelsCompile|ContractSamples|OpenAPIReferencesAndOperations|RefreshCancellationMigration)",
               "-count=1"]
    with log.open("w", encoding="utf-8") as output:
        result = subprocess.run(command, cwd=snapshot, stdout=output, stderr=subprocess.STDOUT)
    lines = log.read_text(encoding="utf-8", errors="replace").splitlines()
    for number, line in enumerate(lines):
        if line.startswith(("ok ", "FAIL", "--- FAIL", "# ")):
            print("\n".join(lines[number:min(number + 5, len(lines))]), flush=True)
    print(f"Validation log: {log}", flush=True)
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())

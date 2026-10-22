"""按审阅后的文件清单提交批量合并功能，保留其他任务的工作树和暂存内容。"""
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

SERVER = Path(__file__).resolve().parents[1]
ROOT = SERVER.parent
OUTPUT = SERVER / 'build/bulk-merge-commit'
REPOSITORIES = {'server': SERVER, 'web': ROOT / 'web', 'parent': ROOT}
MESSAGES = {
    'server': 'feat(matching): 支持按分数和重名重作者批量合并音乐',
    'web': 'feat(library): 增加一键合并重复音乐入口',
    'parent': 'feat(matching): 同步批量合并架构与实现版本',
}
SERVER_FILES = '''docs/architecture.md
docs/contracts/http.schema.json
docs/contracts/openapi.yaml
docs/library/duplicate-detection.md
docs/metadata/matching.md
docs/plugins/storage-plugins.md
plugins/database/sqlite/matching_transactions.go
plugins/database/sqlite/matching_bulk_graph.go
plugins/database/sqlite/matching_bulk_merge.go
src/app/api/matching.go
src/app/api/matching_bulk_merge.go
src/metadata/matching/repository.go
src/metadata/matching/bulk_merge.go
tests/testdata/contracts/api/matching.json
tests/contract/api/matching_bulk_http_test.go
tests/integration/storage/matching_bulk_fixture_test.go
tests/integration/storage/matching_bulk_merge_test.go
tests/integration/storage/matching_bulk_safety_test.go
tests/integration/storage/matching_bulk_transactions_test.go
scripts/prepare-bulk-merge-test.ps1
scripts/update-bulk-merge-contracts.py
scripts/commit-bulk-merge.py'''.splitlines()
SERVER_FILES += [f'src/storage/rpcbackend/client_{i:02}.go' for i in range(8)]
WEB_FILES = '''docs/bulk-merge.md
src/api/client.ts
src/api/matching.ts
src/features/Library.tsx
src/features/matching/MatchingRecovery.tsx
src/features/matching/matching.css
src/features/matching/useDecision.ts
src/features/matching/BulkMerge.tsx
src/i18n/matching.ts
src/library/matching-decisions.ts
tests/e2e/directory-server.ts
tests/e2e/bulk-merge-mock.spec.ts
tests/e2e/bulk-merge-real.spec.ts
tests/unit/bulk-merge.test.ts'''.splitlines()


def git(repo, *args, data=None, index=None):
    env = os.environ.copy()
    env.pop('GIT_INDEX_FILE', None)
    if index:
        env['GIT_INDEX_FILE'] = str(index)
    return subprocess.check_output(['git', *args], cwd=repo, input=data, env=env)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def one_line(text, prefix):
    matches = [line for line in text.splitlines() if line.startswith(prefix)]
    if len(matches) != 1:
        raise RuntimeError(f'Expected one matching paragraph: {prefix}')
    return matches[0]


def snapshot(repo, name, head):
    current = (repo / name).read_bytes()
    if repo == SERVER and name == 'docs/contracts/openapi.yaml':
        base = git(repo, 'show', f'{head}:{name}').decode('utf-8')
        value = current.decode('utf-8').replace('\r\n', '\n')
        prefix = '    "/plugin-settings/{pluginId}":'
        value = value.replace(one_line(value, prefix), one_line(base, prefix), 1)
        return value.encode('utf-8')
    if repo == ROOT and name == 'docs/architecture.md':
        base = git(repo, 'show', f'{head}:{name}').decode('utf-8')
        value = current.decode('utf-8')
        prefix = 'Web 曲目列表由 Server 在分页前'
        old, new = one_line(base, prefix), one_line(value, prefix)
        paragraph = one_line(value, 'Web 的「一键合并重复音乐」')
        if paragraph in base:
            raise RuntimeError('Bulk merge architecture is already committed')
        return base.replace(old, paragraph + '\n\n' + new, 1).encode('utf-8')
    return current


def prepare(area):
    repo = REPOSITORIES[area]
    folder = OUTPUT / area
    folder.mkdir(parents=True, exist_ok=True)
    index = folder / 'index'
    if index.exists():
        raise RuntimeError('Prepared index already exists; inspect it before retrying')
    head = git(repo, 'rev-parse', 'HEAD').decode().strip()
    names = {'server': SERVER_FILES, 'web': WEB_FILES, 'parent': ['docs/architecture.md']}[area]
    if git(repo, 'diff', '--cached', '--name-only', '--', *names).strip():
        raise RuntimeError('Selected paths already contain staged changes')
    git(repo, 'read-tree', head, index=index)
    manifest = {'head': head, 'files': {}, 'gitlinks': {}}
    entries = []
    for name in names:
        raw = snapshot(repo, name, head)
        saved = folder / 'files' / name
        saved.parent.mkdir(parents=True, exist_ok=True)
        saved.write_bytes(raw)
        manifest['files'][name] = {'source': digest((repo / name).read_bytes()), 'snapshot': digest(raw)}
        blob = git(repo, 'hash-object', '-w', '--stdin', '--path=' + name, data=raw).decode().strip()
        entries.append(f'100644 {blob}\t{name}\n')
    if area == 'parent':
        for child in ('server', 'web'):
            record = json.loads((OUTPUT / child / 'committed.json').read_text(encoding='utf-8'))
            actual = git(REPOSITORIES[child], 'rev-parse', 'HEAD').decode().strip()
            if actual != record['commit']:
                raise RuntimeError(f'Unexpected child HEAD: {child}')
            manifest['gitlinks'][child] = actual
            entries.append(f'160000 {actual}\t{child}\n')
    git(repo, 'update-index', '--index-info', data=''.join(entries).encode(), index=index)
    git(repo, 'diff', '--cached', '--check', index=index)
    (folder / 'manifest.json').write_text(json.dumps(manifest, indent=2), encoding='utf-8')
    print(git(repo, 'diff', '--cached', '--stat', index=index).decode('utf-8'))
    print(f'Prepared {area} commit with a separate index')


def commit(area):
    repo, folder = REPOSITORIES[area], OUTPUT / area
    manifest = json.loads((folder / 'manifest.json').read_text(encoding='utf-8'))
    if git(repo, 'rev-parse', 'HEAD').decode().strip() != manifest['head']:
        raise RuntimeError('HEAD changed after preparation')
    names = list(manifest['files']) + list(manifest['gitlinks'])
    if git(repo, 'diff', '--cached', '--name-only', '--', *names).strip():
        raise RuntimeError('Selected paths were staged after preparation')
    for name, expected in manifest['files'].items():
        if digest((repo / name).read_bytes()) != expected['source']:
            raise RuntimeError(f'Working file changed: {name}')
        if digest((folder / 'files' / name).read_bytes()) != expected['snapshot']:
            raise RuntimeError(f'Snapshot changed: {name}')
    for name, expected in manifest['gitlinks'].items():
        if git(ROOT / name, 'rev-parse', 'HEAD').decode().strip() != expected:
            raise RuntimeError(f'Child HEAD changed: {name}')
    print(git(repo, '-c', 'commit.gpgsign=false', 'commit', '--no-gpg-sign', '-m', MESSAGES[area], index=folder / 'index').decode('utf-8'))
    revision = git(repo, 'rev-parse', 'HEAD').decode().strip()
    (folder / 'committed.json').write_text(json.dumps({'commit': revision}), encoding='utf-8')
    # 只同步本次路径的索引，其他任务已暂存的内容保持原状。
    git(repo, 'reset', '--quiet', 'HEAD', '--', *names)
    print(f'Committed {area}: {revision}')


if __name__ == '__main__':
    if len(sys.argv) != 3 or sys.argv[1] not in ('prepare', 'commit') or sys.argv[2] not in REPOSITORIES:
        raise SystemExit('Usage: commit-bulk-merge.py prepare|commit server|web|parent')
    {'prepare': prepare, 'commit': commit}[sys.argv[1]](sys.argv[2])

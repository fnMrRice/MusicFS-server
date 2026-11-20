"""从已捕获的损坏快照准备离线恢复候选，不修改运行中的数据库。"""

import argparse
import hashlib
import json
import shutil
import sqlite3
import struct
from pathlib import Path


def readonly(path):
    return sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)


def quote(name):
    return '"' + name.replace('"', '""') + '"'


def rows_digest(rows):
    digest = hashlib.sha256()
    for row in rows:
        for value in row:
            if isinstance(value, bytes):
                encoded = b"B" + value
            else:
                encoded = json.dumps(value, ensure_ascii=False).encode("utf-8")
            digest.update(struct.pack(">Q", len(encoded)))
            digest.update(encoded)
    return digest.hexdigest()


def salvage_job(snapshot, wal_path, root_page, job_id, scratch):
    # 旧 WAL 帧仅用于抢救任务计划，不把它当作当前已提交状态。
    wal = wal_path.read_bytes()
    page_size = struct.unpack(">I", wal[8:12])[0]
    if page_size != 4096 or len(wal) < 32:
        raise ValueError("Unsupported WAL page size")
    stride = page_size + 24
    for offset in reversed(range(32, len(wal) - stride + 1, stride)):
        if struct.unpack(">I", wal[offset:offset + 4])[0] != root_page:
            continue
        page = wal[offset + 24:offset + stride]
        if page[0] != 13 or struct.unpack(">H", page[3:5])[0] != 1:
            continue
        shutil.copyfile(snapshot, scratch)
        with scratch.open("r+b") as handle:
            handle.seek((root_page - 1) * page_size)
            handle.write(page)
        connection = readonly(scratch)
        try:
            connection.row_factory = sqlite3.Row
            rows = connection.execute("SELECT * FROM jobs NOT INDEXED").fetchall()
            if len(rows) == 1 and rows[0]["id"] == job_id:
                return dict(rows[0]), (offset - 32) // stride
        finally:
            connection.close()
    raise ValueError("No matching job could be recovered from captured WAL")


def prepare(snapshot, wal, destination):
    # 仅接受此次已确认的两张表损坏，遇到其他损坏停止，不静默丢弃数据。
    destination.mkdir(parents=True, exist_ok=False)
    source = readonly(snapshot)
    schema = source.execute(
        "SELECT type,name,rootpage,sql FROM sqlite_master "
        "WHERE sql IS NOT NULL ORDER BY rowid"
    ).fetchall()
    tables = {name: (root, sql) for kind, name, root, sql in schema if kind == "table"}
    contents = {}
    damaged = []
    for name in tables:
        try:
            contents[name] = source.execute(
                "SELECT * FROM " + quote(name) + " NOT INDEXED ORDER BY rowid"
            ).fetchall()
        except sqlite3.DatabaseError:
            damaged.append(name)
    if set(damaged) != {"jobs", "provider_scan_leases"}:
        raise ValueError("Unexpected damaged tables: " + ", ".join(damaged))
    generations = source.execute(
        "SELECT DISTINCT scan_job_id,scan_generation,plan_digest FROM metadata_automation"
    ).fetchall()
    if len(generations) != 1:
        raise ValueError("Recovery requires one independently evidenced scan generation")
    job_id, generation, plan_digest = generations[0]
    if source.execute("SELECT COUNT(*) FROM jobs INDEXED BY sqlite_autoindex_jobs_1").fetchone()[0] != 1:
        raise ValueError("Recovery requires exactly one job in the surviving job index")
    job, frame = salvage_job(snapshot, wal, tables["jobs"][0], job_id,
                             destination / "job-page-salvage.sqlite")
    if job["plan_digest"] != plan_digest or job["state"] != "queued":
        raise ValueError("Recovered plan does not match scan publication evidence")
    plan = job["plan"] if isinstance(job["plan"], bytes) else job["plan"].encode("utf-8")
    if hashlib.sha256(b"musicfs-scan-plan-v1\0" + plan).hexdigest() != plan_digest:
        raise ValueError("Recovered plan digest is invalid")
    observed = source.execute(
        "SELECT COUNT(*) FROM tracks WHERE owner=? AND provider=?", (job["owner"], job["target"])
    ).fetchone()[0]
    published = source.execute(
        "SELECT COUNT(DISTINCT source_id) FROM metadata_automation WHERE scan_job_id=?", (job_id,)
    ).fetchone()[0]
    if observed != published or observed == 0:
        raise ValueError("Published tracks and automation evidence do not agree")
    # 完成状态已经丢失，不能伪造成功；保留失败任务并等待显式重扫。
    job.update(state="failed", generation=generation, observed=observed, complete=0,
               attempt_count=1, next_attempt_at=None, last_success_at=None,
               error=json.dumps({"code": "TEMPORARY_FAILURE", "message":
                                 "Scan state recovered after database corruption; rescan required"}))
    job_columns = [row[1] for row in source.execute("PRAGMA table_info(jobs)")]
    contents["jobs"] = [tuple(job[column] for column in job_columns)]
    contents["provider_scan_leases"] = [(job["owner"], job["target"], generation, None)]
    target_path = destination / "recovered.sqlite"
    target = sqlite3.connect(target_path)
    try:
        target.execute("BEGIN")
        for name, (_, sql) in tables.items():
            if name != "sqlite_sequence":
                target.execute(sql)
        for name, rows in contents.items():
            if name == "sqlite_sequence":
                target.execute("DELETE FROM sqlite_sequence")
            if rows:
                placeholders = ",".join("?" for _ in rows[0])
                target.executemany("INSERT INTO " + quote(name) + " VALUES (" + placeholders + ")", rows)
        for kind, _, _, sql in schema:
            if kind != "table":
                target.execute(sql)
        target.commit()
        integrity = target.execute("PRAGMA integrity_check").fetchall()
        foreign_keys = target.execute("PRAGMA foreign_key_check").fetchall()
        if integrity != [("ok",)] or foreign_keys:
            raise ValueError("Recovered database failed integrity or foreign key checks")
        preserved = {}
        for name, rows in contents.items():
            actual = target.execute("SELECT * FROM " + quote(name) + " ORDER BY rowid").fetchall()
            if rows != actual:
                raise ValueError("Recovered rows differ in table " + name)
            if name not in damaged:
                query = "SELECT rowid,* FROM " + quote(name) + " ORDER BY rowid"
                if source.execute(query).fetchall() != target.execute(query).fetchall():
                    raise ValueError("Implicit row identities changed in table " + name)
                preserved[name] = {"rows": len(rows), "sha256": rows_digest(rows)}
        report = {
            "snapshot": str(snapshot.resolve()), "candidate": str(target_path.resolve()),
            "snapshotSha256": hashlib.sha256(snapshot.read_bytes()).hexdigest(),
            "walSha256": hashlib.sha256(wal.read_bytes()).hexdigest(),
            "integrity": "ok", "foreignKeyViolations": 0,
            "jobId": job_id, "generation": generation, "observed": observed,
            "salvagedPlanFrame": frame, "damagedTables": damaged,
            "reconstruction": "Job marked failed; provider lease released. Explicit rescan required.",
            "preservedTables": preserved,
        }
        (destination / "recovery-report.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
        print(json.dumps({"candidate": str(target_path.resolve()), "integrity": "ok",
                          "tracks": observed, "preservedTables": len(preserved),
                          "liveDatabaseModified": False}))
    finally:
        target.close()
        source.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Prepare an offline scan-state recovery candidate")
    parser.add_argument("snapshot", type=Path)
    parser.add_argument("wal", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    prepare(args.snapshot, args.wal, args.destination)

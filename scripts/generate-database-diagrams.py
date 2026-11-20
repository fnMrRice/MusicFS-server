#!/usr/bin/env python3
"""在内存重放 SQLite 迁移，生成按领域划分的物理表 PlantUML 图。"""

from collections import defaultdict
from pathlib import Path
import re
import sqlite3


ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "docs/diagrams"
GROUPS = {
    "catalog": ("音乐资料", """
music_tracks 曲目资料
music_artists 歌手
music_artist_localized_names 歌手多语言别名
music_track_artists 曲目署名
music_albums 音乐专辑
music_manual_album_tracks 手工专辑成员
music_sources 曲目音源
music_source_providers 来源实例与插件
music_album_source_refs 专辑来源成员依据
music_album_scraped_fields 专辑刮削字段
music_album_scraped_track_refs 发行版刮削曲序
music_album_merged_slots 专辑合并曲位
"""),
    "assets": ("来源、资产与识别证据", """
tracks 来源观测
source_assets 不可变资产版本
asset_evidence 资产证据
hash_jobs 摘要任务
hash_conflicts 摘要冲突
fingerprint_jobs 指纹任务
fingerprint_indexes 指纹索引
retired_fingerprint_evidence 旧指纹证据归档
"""),
    "matching": ("录音匹配与播放恢复", """
recordings 录音版本
recording_source_links 录音来源关联
recording_default_sources 默认来源偏好
recording_cover_preferences 录音封面偏好
match_exclusions 匹配排除
matching_candidates 匹配候选
matching_revisions 匹配修订
matching_runs 匹配运行状态
matching_action_receipts 匹配操作回执
link_changes 关联变更记录
timeline_evidence 播放时间映射证据
processed_cache 处理缓存索引
"""),
    "albums-works": ("来源专辑、发行与作品包", """
source_albums 来源专辑
source_album_members 来源专辑成员
releases 发行集合
release_albums 发行专辑成员
album_cover_bindings 专辑封面绑定
album_cover_heads 当前专辑封面
album_changes 专辑变更记录
works 作品
work_external_ids 作品外部编号
work_credits 作品署名
work_tags 作品标签
work_packages 作品包副本
"""),
    "playlists": ("歌单、收藏与播放历史", """
personal_playlists 个人歌单
playlist_links 关联平台歌单
playlist_link_additions 待同步曲目
recommendation_pages 每日推荐与历史页
music_playlist_entries 共享音乐条目
personal_playlist_snapshots 个人歌单快照
playlist_hidden_rules 隐藏规则
playlist_changes 歌单变更记录
remote_playlists 远程歌单绑定
remote_sources 远程来源资料
remote_playlist_snapshots 远程歌单快照
remote_sync_jobs 远程同步任务
personal_collection_revisions 特殊歌单修订
personal_collection_cursors 特殊歌单游标
personal_playback_receipts 播放上报回执
content_history_pending 待交付内容历史
"""),
    "media": ("媒体内容、发布与备份", """
media_publications 媒体发布阶段
media_content_objects 媒体内容索引
media_content_bindings 内容绑定历史
media_content_slots 当前内容选择
media_selection_revisions 内容选择修订
media_selection_changes 内容选择变更
media_derived_objects 派生媒体索引
media_garbage 媒体回收状态
media_backups 媒体备份任务
media_backup_objects 备份对象清单
"""),
    "metadata": ("资料补全、自动化与原件写回", """
enrichment_jobs 资料补全任务
enrichment_targets 补全目标
enrichment_candidates 补全候选
metadata_automation 自动补全触发
metadata_automation_actions 自动补全动作
writeback_jobs 原件写回任务
writeback_operations 写回幂等记录
"""),
    "scanning": ("扫描、查询与固定快照", """
jobs 扫描任务
provider_scan_leases 来源扫描租约
refresh_task_cancellations 目录刷新取消意图
library_revision 曲库修订
library_snapshots 来源查询快照
library_snapshot_items 来源快照条目
library_cursors 来源快照游标
catalog_queries 曲库查询快照
catalog_query_items 曲库查询条目
catalog_query_cursors 曲库查询游标
"""),
    "system": ("用户、会话与配置", """
users 用户
auth_sessions 登录会话
personal_profiles 个人资料
playback_sessions 播放会话
idempotency 通用幂等记录
config_operations 实例配置操作
plugin_operations 插件管理操作
plugin_settings 用户插件设置
schema_migrations 数据库迁移版本
"""),
}
LABELS = {
    name: label
    for _, raw in GROUPS.values()
    for name, label in (line.split(maxsplit=1) for line in raw.strip().splitlines())
}


def quote(identifier):
    return '"' + identifier.replace('"', '""') + '"'


def load_schema():
    """只使用内存数据库，不读取或写入运行数据。"""
    db = sqlite3.connect(":memory:")
    db.execute("PRAGMA foreign_keys=ON")
    store = (ROOT / "plugins/database/sqlite/store.go").read_text(encoding="utf-8")
    ddl = re.search(r'"(CREATE TABLE IF NOT EXISTS schema_migrations\([^"]+)"', store)
    if not ddl:
        raise ValueError("Cannot find the schema_migrations DDL")
    db.execute(ddl[1])
    steps = []
    for path in sorted((ROOT / "plugins/database/sqlite/migrations").glob("*.sql")):
        raw = path.read_text(encoding="utf-8")
        sections = re.split(r"(?m)^-- migration: (\d+_[^\n]+\.sql)\n", raw)
        if len(sections) == 1:
            steps.append((int(path.name.split("_")[0]), raw))
        else:
            if sections[0].strip():
                raise ValueError(f"SQL precedes the first migration marker: {path.name}")
            if int(sections[-2].split("_")[0]) != int(path.name.split("_")[0]):
                raise ValueError(f"Migration batch end does not match: {path.name}")
            steps.extend((int(sections[i].split("_")[0]), sections[i + 1])
                         for i in range(1, len(sections), 2))
    for expected, (version, sql) in enumerate(sorted(steps), start=1):
        if expected != version or not sql.strip():
            raise ValueError(f"Invalid migration sequence at version {expected}")
        db.executescript(sql)
        db.execute("INSERT INTO schema_migrations VALUES (?)", (version,))
    if not steps or db.execute("PRAGMA foreign_key_check").fetchall():
        raise ValueError("Migration replay failed the foreign key check")
    tables = {r[0] for r in db.execute(
        "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'")}
    if tables != LABELS.keys():
        raise ValueError(f"Update diagram groups: missing={tables - LABELS.keys()}, "
                         f"removed={LABELS.keys() - tables}")
    return db, max(v for v, _ in steps)


def columns(db, table):
    return db.execute(f"PRAGMA table_info({quote(table)})").fetchall()


def foreign_keys(db, table):
    keys = defaultdict(list)
    for row in db.execute(f"PRAGMA foreign_key_list({quote(table)})"):
        keys[row[0]].append(row)
    return [sorted(rows, key=lambda row: row[1]) for rows in keys.values()]


def header(title, version):
    suffix = f"（迁移 {version:03d}）" if version is not None else "（独立建表）"
    return [
        "@startuml", f"title MusicFS 数据库 · {title}{suffix}",
        "' 由 scripts/generate-database-diagrams.py 从当前 SQL 和建表代码生成。",
        "hide circle", "skinparam defaultFontName Microsoft YaHei",
        "skinparam defaultFontSize 12", "skinparam shadowing false",
        "skinparam linetype polyline", "skinparam nodesep 35", "skinparam ranksep 55",
        "skinparam entityBackgroundColor #F0F7FF", "skinparam entityBorderColor #6487AB",
        "skinparam ArrowColor #6487AB", "skinparam packageStyle rectangle",
        "skinparam backgroundColor #FFFFFF",
    ]


def entity(db, table, stub=False, label=None):
    keys = foreign_keys(db, table)
    refs = {r[3] for key in keys for r in key}
    fields = columns(db, table)
    if stub:
        fields = [c for c in fields if c[5]]
    stereotype = " <<跨域引用>> #F1F1F1" if stub else ""
    lines = [f'entity "{label or LABELS[table]}\\n{table}" as {table}{stereotype} {{']
    for _, name, kind, required, _, pk in fields:
        tags = ([f"PK{pk}"] if pk else []) + (["FK"] if name in refs else [])
        suffix = " [" + ", ".join(tags) + "]" if tags else ""
        lines.append(f'  {"*" if required else " "} {name} : {kind}{suffix}')
    lines.append("}")
    return lines


def relation(child, key):
    """箭头表示实际 SQL FK；不推断一对一或可选关系。"""
    parent = key[0][2]
    source = ", ".join(r[3] for r in key)
    target = " → " + ", ".join(r[4] or "PK" for r in key) if len(key) > 1 else ""
    action = " / CASCADE" if key[0][6] == "CASCADE" else ""
    return f'{child} --> {parent} : "{source}{target}{action}"'


def generate(db, version):
    for slug, (title, raw) in GROUPS.items():
        own = [line.split()[0] for line in raw.strip().splitlines()]
        external = sorted({key[0][2] for table in own for key in foreign_keys(db, table)} - set(own))
        lines = header(title, version)
        lines.append(f'package "{title} · 完整字段" {{')
        for table in own:
            lines.extend(entity(db, table))
        lines.append("}")
        if external:
            lines.append('package "其他领域 · 仅列主键" #FAFAFA {')
            for table in external:
                lines.extend(entity(db, table, stub=True))
            lines.append("}")
        for table in own:
            for key in foreign_keys(db, table):
                lines.append(relation(table, key))
        if slug == "catalog":
            lines += [
                'entity "专辑成员汇总\\nmusic_album_tracks" as music_album_tracks <<VIEW>> #FFF2D6 {',
                "  album_id : TEXT", "  track_id : TEXT", "  position : INTEGER", "}",
                'music_album_tracks ..> music_manual_album_tracks : 汇总',
                'music_album_tracks ..> music_album_source_refs : 汇总',
                'music_album_tracks ..> music_album_scraped_track_refs : 刮削曲序优先',
                'music_album_tracks ..> music_sources : 解析曲目',
                'music_album_tracks ..> music_albums : 校验归属',
            ]
        lines += [
            "legend bottom left",
            "  PK序号 = 复合主键顺序；FK = SQL 外键列；* = DDL 显式 NOT NULL",
            "  实线箭头：子表 → 被引用表；标签包含复合列及级联删除",
            "  灰色表仅为跨域引用；虚线仅表示视图依赖",
            "  未画业务逻辑引用、唯一索引、CHECK、默认值和触发器；以 SQL 为准",
            "endlegend", "@enduml", "",
        ]
        path = OUTPUT / f"database-{slug}.puml"
        path.write_text("\n".join(lines), encoding="utf-8")
        print(f"Generated {path.relative_to(ROOT)} ({len(own)} tables)")
    state = (ROOT / "plugins/database/sqlite/state/store.go").read_text(encoding="utf-8")
    ddl = re.search(r"`(CREATE TABLE IF NOT EXISTS plugin_values\([^`]+)`", state)
    if not ddl:
        raise ValueError("Cannot find the plugin_values DDL")
    with sqlite3.connect(":memory:") as private:
        private.execute(ddl[1])
        lines = header("独立实例状态库", None)
        lines += entity(private, "plugin_values", label="插件私有状态")
    lines += [
        "note right of plugin_values",
        "  独立于主数据库，不建立跨库外键。",
        "  scope = 插件 ID + NUL + 实例 UUID。",
        "  value 保存经校验的 JSON，revision 用于原子比较更新。",
        "end note", "@enduml", "",
    ]
    (OUTPUT / "database-state.puml").write_text("\n".join(lines), encoding="utf-8")
    print(f"Verified schema {version:03d}: {len(LABELS)} main tables, 1 private-state table")


if __name__ == "__main__":
    connection, latest = load_schema()
    try:
        generate(connection, latest)
    finally:
        connection.close()

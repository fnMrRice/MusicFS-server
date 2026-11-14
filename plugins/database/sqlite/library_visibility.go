package sqlite

// 普通音乐浏览从作品包的音频节点构建排除集合；显式来源与同位置的扫描来源都应排除。
// CTE 只读取当前用户的包，并在每次查询中物化一次，避免逐曲目重复解析目录树。
const ordinaryMusicSources = `WITH excluded_work_audio AS MATERIALIZED (
 SELECT json_extract(n.value,'$.sourceTrackId') AS source_id,
        p.provider_id,p.root_id,json_extract(n.value,'$.sourcePath') AS source_path
 FROM work_packages p JOIN works w ON w.id=p.work_id AND w.owner=p.owner,
      json_each(p.document,'$.nodes') n
 WHERE p.owner=? AND w.content_type<>'music' AND w.show_in_track_list=0
   AND json_extract(n.value,'$.kind')='audio'
) `

// 调用方的来源表统一使用 t 别名；位置匹配不扩展为目录前缀，避免隐藏包外文件。
const ordinaryMusicTrack = `NOT EXISTS (
 SELECT 1 FROM excluded_work_audio e WHERE e.source_id=t.id
) AND NOT EXISTS (
 SELECT 1 FROM excluded_work_audio e
 WHERE e.provider_id=t.provider AND e.root_id=t.root_id AND e.source_path=t.path
)`

// 无成员的人工专辑保留；有成员的专辑至少需要一首普通音乐才出现在浏览列表。
const ordinaryMusicAlbum = `(NOT EXISTS (
 SELECT 1 FROM source_album_members m JOIN tracks t ON t.id=m.source_id
 WHERE m.album_id=a.id AND t.owner=a.owner AND t.provider=a.provider_id
) OR EXISTS (
 SELECT 1 FROM source_album_members m JOIN tracks t ON t.id=m.source_id
 WHERE m.album_id=a.id AND t.owner=a.owner AND t.provider=a.provider_id AND ` + ordinaryMusicTrack + `
))`

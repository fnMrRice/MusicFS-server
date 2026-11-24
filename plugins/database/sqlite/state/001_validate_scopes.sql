-- 按字节拆分插件名与实例 UUID，SQLite 文本函数会把 NUL 当作字符串结尾。
WITH scopes AS (
    SELECT DISTINCT CAST(scope AS BLOB) AS raw FROM plugin_values
), parts AS (
    SELECT instr(raw, x'00') AS separator,
           CAST(substr(raw, 1, instr(raw, x'00') - 1) AS TEXT) AS plugin,
           substr(raw, instr(raw, x'00') + 1) AS instance_bytes
    FROM scopes
), identities AS (
    SELECT separator, plugin, instance_bytes, CAST(instance_bytes AS TEXT) AS id FROM parts
)
SELECT EXISTS (
    SELECT 1 FROM identities
    WHERE separator < 2 OR plugin IN (
        'musicfs.localfiles', 'musicfs.netease', 'musicfs.qqmusic', 'musicfs.applemusic',
        'musicfs.audio.ffmpeg', 'musicfs.audioffmpeg', 'musicfs.ncm'
    ) OR length(instance_bytes) <> 36 OR instr(instance_bytes, x'00') <> 0
      OR id NOT GLOB '????????-????-????-????-????????????'
      OR length(replace(id, '-', '')) <> 32 OR replace(id, '-', '') GLOB '*[^0-9a-f]*'
      OR id = '00000000-0000-0000-0000-000000000000'
);

-- 仅规范化有效的歌手字符串数组；固定查询快照和曲目来源、关系保持原样。
WITH RECURSIVE
documents AS MATERIALIZED (
 SELECT id,document FROM tracks WHERE json_valid(document)
),
arrays AS MATERIALIZED (
 SELECT id,json_extract(document,'$.artists') AS artists FROM documents
 WHERE json_type(document,'$.artists')='array'
),
valid_arrays AS MATERIALIZED (
 SELECT id,artists FROM arrays
 WHERE NOT EXISTS(SELECT 1 FROM json_each(arrays.artists) WHERE type<>'text')
),
parts(id,artist_position,part_position,name,remainder) AS (
 -- 按字节定位 ASCII 分隔符，避免 SQLite 文本截取在内嵌 NUL 处截短名字。
 SELECT valid_arrays.id,artist.key,0,'',CAST(artist.value AS BLOB)
 FROM valid_arrays,json_each(valid_arrays.artists) AS artist
 UNION ALL
 SELECT id,artist_position,part_position+1,
        CAST(CASE WHEN instr(remainder,x'2f')=0 THEN remainder ELSE substr(remainder,1,instr(remainder,x'2f')-1) END AS TEXT),
        CASE WHEN instr(remainder,x'2f')=0 THEN x'' ELSE substr(remainder,instr(remainder,x'2f')+1) END
 FROM parts WHERE remainder<>x''
),
trimmed AS (
 -- 与 Go strings.TrimSpace 的 Unicode White_Space 字符集合一致。
 SELECT id,artist_position,part_position,
        trim(name,char(9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288)) AS name
 FROM parts WHERE part_position>0
),
ranked AS (
 SELECT *,row_number() OVER(PARTITION BY id,name ORDER BY artist_position,part_position) AS occurrence
 FROM trimmed WHERE name<>''
),
normalized AS MATERIALIZED (
 SELECT valid_arrays.id,valid_arrays.artists AS previous,
        (SELECT json_group_array(name) FROM (
         SELECT name FROM ranked WHERE ranked.id=valid_arrays.id AND occurrence=1
         ORDER BY artist_position,part_position
        )) AS artists
 FROM valid_arrays
),
changed AS MATERIALIZED (
 SELECT id,artists FROM normalized
 WHERE json_array_length(previous)<>json_array_length(artists)
    OR EXISTS(SELECT 1 FROM json_each(previous) AS original
              WHERE original.value<>json_extract(artists,'$['||original.key||']'))
)
UPDATE tracks SET document=json_set(document,'$.artists',json(changed.artists))
 FROM changed WHERE tracks.id=changed.id;

-- 只有实际修改歌手时才递增曲库修订，让后续新查询观察到变化。
UPDATE library_revision SET revision=revision+1 WHERE changes()>0;

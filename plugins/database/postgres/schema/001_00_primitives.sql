-- 元数据快照以 UTC 毫秒文本保存，保持排序和导出格式一致。
CREATE FUNCTION musicfs_timestamp() RETURNS text
LANGUAGE sql VOLATILE AS $$
    SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
$$;

-- 部分任务和观测记录允许携带非 JSON 文本。有条件的 JSON 查询跳过这些值。
CREATE FUNCTION musicfs_parse_json(value text) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
    RETURN value::jsonb;
EXCEPTION WHEN data_exception THEN
    RETURN NULL;
END;
$$;

CREATE FUNCTION musicfs_observation_metadata(document text) RETURNS jsonb
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
    SELECT jsonb_object_agg(key,value) FILTER (WHERE key IN ('title','displayName','artists','album','genre','year'))
    FROM jsonb_each(CASE WHEN jsonb_typeof(musicfs_parse_json(document))='object'
                       THEN musicfs_parse_json(document) ELSE '{}'::jsonb END)
$$;

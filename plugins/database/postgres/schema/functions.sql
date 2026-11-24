-- 共享仓储使用的 JSON 路径只包含对象字段及非负数组下标。
CREATE FUNCTION musicfs_json_path(path text) RETURNS text[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT CASE WHEN path = '$' THEN ARRAY[]::text[]
    ELSE string_to_array(substr(regexp_replace(path, '\[([0-9]+)\]', '.\1', 'g'), 3), '.') END
$$;

CREATE FUNCTION musicfs_json_text(doc text, path text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT CASE v WHEN 'true'::jsonb THEN '1' WHEN 'false'::jsonb THEN '0' ELSE v #>> '{}' END
  FROM (SELECT doc::jsonb #> musicfs_json_path(path) AS v) input
$$;

CREATE FUNCTION musicfs_json_valid(doc text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
BEGIN
  PERFORM doc::jsonb;
  RETURN true;
EXCEPTION WHEN data_exception THEN RETURN false;
END
$$;

CREATE FUNCTION musicfs_json_type(doc text, path text DEFAULT '$') RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT CASE jsonb_typeof(v)
    WHEN 'number' THEN CASE WHEN v::text ~ '^-?[0-9]+$' THEN 'integer' ELSE 'real' END
    WHEN 'string' THEN 'text' WHEN 'boolean' THEN v::text ELSE jsonb_typeof(v) END
  FROM (SELECT doc::jsonb #> musicfs_json_path(path) AS v) input
$$;

CREATE FUNCTION musicfs_json_array_length(doc text) RETURNS bigint
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT CASE WHEN jsonb_typeof(doc::jsonb)='array' THEN jsonb_array_length(doc::jsonb) ELSE 0 END
$$;

-- 数组保留原下标。业务查询读取对象的值时只按键排序，用同一数值列表示键的排序位置。
CREATE FUNCTION musicfs_json_each(doc text, path text DEFAULT '$') RETURNS TABLE(key bigint, value text)
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  WITH input AS (SELECT doc::jsonb #> musicfs_json_path(path) AS data)
  SELECT a.ordinality-1, a.item #>> '{}' FROM input
    CROSS JOIN LATERAL jsonb_array_elements(CASE jsonb_typeof(data) WHEN 'array' THEN data END)
      WITH ORDINALITY AS a(item, ordinality)
  UNION ALL
  SELECT row_number() OVER (ORDER BY obj.key COLLATE "C")-1, obj.value #>> '{}' FROM input
    CROSS JOIN LATERAL jsonb_each(CASE jsonb_typeof(data) WHEN 'object' THEN data END) obj
  UNION ALL
  SELECT NULL::bigint, data #>> '{}' FROM input WHERE jsonb_typeof(data) NOT IN ('array','object')
$$;

-- 缺失的父对象需要逐级建立，这与作品首次写入刮削字段时的 JSON 更新语义一致。
CREATE FUNCTION musicfs_json_set(doc text, path text, value jsonb) RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE
  result jsonb := doc::jsonb;
  keys text[] := musicfs_json_path(path);
  prefix text[];
  i integer;
BEGIN
  FOR i IN 1..cardinality(keys)-1 LOOP
    prefix := keys[1:i];
    IF result #> prefix IS NULL THEN
      result := jsonb_set(result, prefix, '{}'::jsonb);
    END IF;
  END LOOP;
  RETURN jsonb_set(result, keys, coalesce(value, 'null'::jsonb))::text;
END
$$;

-- 作品元数据按 JSON Merge Patch 逐字段合并，null 表示移除对象字段。
CREATE FUNCTION musicfs_json_patch(doc jsonb, patch jsonb) RETURNS jsonb
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
  result jsonb := CASE WHEN jsonb_typeof(doc)='object' THEN doc ELSE '{}'::jsonb END;
  field record;
BEGIN
  IF jsonb_typeof(patch) <> 'object' THEN RETURN patch; END IF;
  FOR field IN SELECT * FROM jsonb_each(patch) LOOP
    IF field.value = 'null'::jsonb THEN
      result := result - field.key;
    ELSE
      result := jsonb_set(result, ARRAY[field.key],
        musicfs_json_patch(coalesce(result -> field.key, 'null'::jsonb), field.value));
    END IF;
  END LOOP;
  RETURN result;
END
$$;

CREATE FUNCTION musicfs_julianday(value text) RETURNS double precision
LANGUAGE sql STABLE STRICT PARALLEL SAFE AS $$
  SELECT extract(epoch FROM (CASE WHEN value='now' THEN statement_timestamp() ELSE value::timestamptz END))::double precision / 86400 + 2440587.5
$$;

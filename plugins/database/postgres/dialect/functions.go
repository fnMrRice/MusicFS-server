package dialect

import "strings"

func renderCall(name string, args []string) string {
	body := strings.Join(args, ", ")
	switch strings.ToLower(name) {
	case "(":
		return "(" + body + ")"
	case "instr":
		name = "strpos"
	case "lower":
		// 曲库搜索沿用 SQLite 的 ASCII 大小写规则，避免依赖数据库集群的区域设置。
		return `lower((` + body + `) COLLATE "C")`
	case "json":
		return "(" + body + ")::jsonb"
	case "json_valid", "json_each", "json_type", "json_array_length", "julianday":
		name = "musicfs_" + strings.ToLower(name)
	case "json_extract":
		name = "musicfs_json_text"
	case "json_group_array":
		return "(coalesce(jsonb_agg(" + body + "), '[]'::jsonb))::text"
	case "json_object":
		name = "jsonb_build_object"
	case "json_patch":
		return "(musicfs_json_patch((" + args[0] + ")::jsonb, (" + args[1] + ")::jsonb))::text"
	case "json_set":
		return jsonSet(args)
	case "max", "min":
		if len(args) > 1 {
			if strings.EqualFold(name, "max") {
				name = "greatest"
			} else {
				name = "least"
			}
		}
	case "unixepoch":
		if body == "" {
			return "floor(extract(epoch from statement_timestamp()))::bigint"
		}
	case "strftime":
		if len(args) == 2 && args[0] == "'%Y-%m-%dT%H:%M:%fZ'" && args[1] == "'now'" {
			return `to_char(statement_timestamp() AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')`
		}
	}
	return name + "(" + body + ")"
}

func jsonSet(args []string) string {
	document := args[0]
	for i := 1; i+1 < len(args); i += 2 {
		path, value := args[i], args[i+1]
		// 绑定参数和字符串常量在 SQL 解析阶段没有类型。非字符串值由调用方显式转换。
		if strings.HasPrefix(value, "$") || strings.HasPrefix(value, "'") {
			value = "(" + value + ")::text"
		}
		document = "musicfs_json_set(" + document + ", " + path + ", to_jsonb(" + value + "))"
	}
	return document
}

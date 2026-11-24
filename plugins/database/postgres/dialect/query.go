package dialect

import (
	"strconv"
	"strings"
)

// 查询均为仓储源码中的 SQL。按 SQL 词法识别参数和函数，字符串和引用标识符中的内容原样保留。
func translate(query string) string {
	tokens := tokenize(query)
	cursor := 0
	return render(tokens, &cursor, false)
}

func identifier(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$' || c == '.'
}

func tokenize(sql string) []string {
	var result []string
	parameter := 0
	for i := 0; i < len(sql); {
		start := i
		c := sql[i]
		i++
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			continue
		case c == '-' && i < len(sql) && sql[i] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i < len(sql) && sql[i] == '*':
			end := strings.Index(sql[i+1:], "*/")
			if end < 0 {
				return append(result, sql[start:])
			}
			i += end + 3
			continue
		case c == '\'' || c == '"':
			for i < len(sql) {
				curr := sql[i]
				i++
				if curr == c {
					if i == len(sql) || sql[i] != c {
						break
					}
					i++
				}
			}
		case c == '?':
			for i < len(sql) && sql[i] >= '0' && sql[i] <= '9' {
				i++
			}
			n := parameter + 1
			if i > start+1 {
				n, _ = strconv.Atoi(sql[start+1 : i])
			}
			parameter = max(n, parameter)
			result = append(result, "$"+strconv.Itoa(n))
			continue
		case identifier(c):
			for i < len(sql) && identifier(sql[i]) {
				i++
			}
		case i < len(sql) && (c == '<' && sql[i] == '>' || strings.ContainsRune("!<>", rune(c)) && sql[i] == '=' || c == '|' && sql[i] == '|' || c == ':' && sql[i] == ':'):
			i++
		}
		result = append(result, sql[start:i])
	}
	return result
}

func at(tokens []string, i int, word string) bool {
	return i >= 0 && i < len(tokens) && strings.EqualFold(tokens[i], word)
}

// 递归读取括号，只有函数调用的顶层逗号才分隔参数。
func render(tokens []string, cursor *int, argument bool) string {
	var parts []string
	for *cursor < len(tokens) {
		i := *cursor
		name := tokens[i]
		if name == ")" || argument && name == "," {
			break
		}
		*cursor = i + 1
		switch {
		case at(tokens, i, "limit") && at(tokens, i+1, "-") && at(tokens, i+2, "1"):
			name = "LIMIT ALL"
			*cursor += 2
		case at(tokens, i, "cross") && at(tokens, i+1, "join") && at(tokens, i+4, "on"):
			// SQLite 用 CROSS JOIN ... ON 固定访问顺序。PostgreSQL 的有条件连接使用 JOIN。
			continue
		case at(tokens, i+1, "collate") && at(tokens, i+2, "nocase"):
			name = `lower(` + name + ` COLLATE "C")`
			*cursor += 2
		case name == "(" || at(tokens, i+1, "("):
			if name != "(" {
				*cursor++
			}
			args := []string{render(tokens, cursor, true)}
			for at(tokens, *cursor, ",") {
				*cursor++
				args = append(args, render(tokens, cursor, true))
			}
			if at(tokens, *cursor, ")") {
				*cursor++
			}
			// 比较 JSON 标志时保留原生 JSON 类型，让任务查询能命中 PostgreSQL 的部分索引。
			if strings.EqualFold(name, "json_extract") && len(args) == 2 && at(tokens, *cursor, "=") && at(tokens, *cursor+1, "1") {
				name = "(musicfs_parse_json(" + args[0] + ") #> musicfs_json_path(" + args[1] + ") IN ('true'::jsonb,'1'::jsonb))"
				*cursor += 2
			} else {
				name = renderCall(name, args)
			}
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, " ")
}

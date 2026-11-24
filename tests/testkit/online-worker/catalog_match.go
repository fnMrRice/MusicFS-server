package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 两个编号分别对应这次导入的新来源和已独立存在的旧来源，批量查询按实际请求返回。
func catalogMatchSongs(raw string) string {
	var ids []struct{ ID int }
	_ = json.Unmarshal([]byte(raw), &ids)
	titles := map[int]string{42: "New song", 43: "Previous song"}
	songs := make([]string, 0, len(ids))
	for _, requested := range ids {
		if title := titles[requested.ID]; title != "" {
			songs = append(songs, fmt.Sprintf(`{"id":%d,"name":%q,"ar":[{"id":7,"name":"Artist"}],"al":{"id":8,"name":"Album"},"dt":8040}`, requested.ID, title))
		}
	}
	return `{"code":200,"songs":[` + strings.Join(songs, ",") + `]}`
}

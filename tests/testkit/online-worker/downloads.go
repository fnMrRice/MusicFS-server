package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 模拟平台返回的不同音质。43 仅提供标准音质，44 的下载窗口供重启恢复测试使用。
func downloadFixture(r *http.Request) *http.Response {
	body, contentType := "", "application/json"
	switch {
	case r.URL.Host == "m8.music.126.net":
		if strings.HasPrefix(r.URL.Path, "/44-") {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}
		body, contentType = "fixture bytes for "+r.URL.Path, "application/octet-stream"
	case r.URL.Path == "/api/v3/song/detail":
		query := r.URL.Query().Get("c")
		songs := "[]"
		for _, id := range []string{"42", "43", "44"} {
			if strings.Contains(query, id) {
				songs = fmt.Sprintf(`[{"id":%s,"name":"Song %s","ar":[],"al":{},"dt":120000}]`, id, id)
				break
			}
		}
		body = `{"code":200,"songs":` + songs + "}"
	case r.URL.Path == "/api/song/enhance/player/url/v1":
		id := strings.Trim(r.URL.Query().Get("ids"), "[]")
		quality, encoding := r.URL.Query().Get("level"), "mp3"
		if id == "43" {
			quality = "standard"
		}
		if quality == "lossless" {
			encoding = "flac"
		}
		path := "/" + id + "-" + quality + "." + encoding
		payload := "fixture bytes for " + path
		body = fmt.Sprintf(`{"code":200,"data":[{"id":%s,"code":200,"type":%q,"level":%q,"md5":"%x","size":%d,"url":%q}]}`, id, encoding, quality, md5.Sum([]byte(payload)), len(payload), "https://m8.music.126.net"+path)
	default:
		return nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body))}
}

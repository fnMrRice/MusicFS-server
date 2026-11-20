package scraper

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
)

// SettingsProvider 由插件声明配置及校验，并为一次任务派生独立的网络客户端。
// 宿主按用户和插件读取设置，不根据插件编号猜测配置项。
// WithSettings 成功时必须返回非空插件和客户端；客户端连接池归本次任务所有，
// 宿主在查询结束或媒体响应关闭后调用 CloseIdleConnections，不能修改共享传输器。
type SettingsProvider interface {
	SettingsSchema() pluginsettings.Schema
	ValidateSettings(map[string]any) error
	WithSettings(map[string]any) (Plugin, *http.Client, error)
}

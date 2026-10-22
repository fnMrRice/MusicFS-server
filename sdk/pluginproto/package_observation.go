package pluginproto

// PackageObserveRequest 仅由宿主发送已授权且校验版本的音频观测批次。
// 插件不得据此扩展目录、读取任意路径或发布数据库记录。
type PackageObserveRequest struct {
	Entries []Entry `json:"entries"`
}

type PackageObserveResult struct {
	Items []Observation `json:"items"`
}

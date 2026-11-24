// Package audiotypes 定义音频插件与宿主共享的数据契约。
package audiotypes

type Profile struct {
	ID         string `json:"id"`
	MIME       string `json:"mime"`
	Codec      string `json:"codec"`
	Container  string `json:"container"`
	SampleRate int    `json:"sampleRate"`
	Channels   int    `json:"channels"`
	Layout     string `json:"layout"`
	BitrateBps int    `json:"bitrateBps"`
	SeekMode   string `json:"seekMode"`
}

type Plan struct {
	TaskID       string  `json:"taskId"`
	AssetID      string  `json:"assetId"`
	Profile      string  `json:"profile"`
	GainDB       float64 `json:"gainDB"`
	PositionMs   int64   `json:"positionMs"`
	DecodeOutput string  `json:"decodeOutput,omitempty"`
}

type Metadata struct {
	NetEaseID                                           string
	NetEaseIDConflict                                   bool
	Codec, Container                                    *string
	DurationMs, BitrateBps                              *int64
	MIME                                                string
	Title                                               *string
	Artists                                             []string
	Album, Genre                                        *string
	Year, SampleRateHz                                  *int64
	Channels, BitDepth                                  int
	Layout, BitrateOrigin, ProbeVersion, ProbeAlgorithm string
}

// AddNetEaseID 合并容器和音频标签中的编号。多个不同编号意味着证据冲突，后续标签不能覆盖该状态。
func (m *Metadata) AddNetEaseID(id string) {
	if m.NetEaseIDConflict || id == "" {
		return
	}
	if m.NetEaseID != "" && id != m.NetEaseID {
		m.NetEaseID, m.NetEaseIDConflict = "", true
		return
	}
	m.NetEaseID = id
}

// ValidNetEaseID 要求平台的规范十进制曲目编号，不接受零、前导零、符号或浮点数。
func ValidNetEaseID(id string) bool {
	if len(id) == 0 || len(id) > 20 || id[0] == '0' {
		return false
	}
	for i := range len(id) {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// ProcessingIdentity 绑定实际插件和工具链，不假定存在某个外部程序。
type ProcessingIdentity struct {
	WorkerSHA256, DecoderSHA256, PluginVersion, PipelineRevision string
}

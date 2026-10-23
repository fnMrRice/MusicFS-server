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
	Codec, Container                                    *string
	DurationMs, BitrateBps                              *int64
	MIME                                                string
	Title                                               *string
	Artists                                             []string
	Album, Genre                                        *string
	Year, SampleRateHz                                  *int64
	Channels                                            int
	Layout, BitrateOrigin, ProbeVersion, ProbeAlgorithm string
}

// ProcessingIdentity 绑定实际插件和工具链，不假定存在某个外部程序。
type ProcessingIdentity struct {
	WorkerSHA256, DecoderSHA256, PluginVersion, PipelineRevision string
}

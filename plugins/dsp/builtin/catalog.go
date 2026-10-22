package builtin

// Port 描述插件可接受的 PCM 格式；具体输出由节点根据参数协商。
type Port struct {
	SampleFormat string   `json:"sampleFormat"`
	SampleRate   int      `json:"sampleRate"`
	Layouts      []string `json:"layouts"`
}

type OutputPort struct {
	CanContinueDSP bool     `json:"canContinueDSP"`
	SameAsInput    bool     `json:"sameAsInput"`
	Layouts        []string `json:"layouts"`
}

type ParameterOption struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type Parameter struct {
	Key          string            `json:"key"`
	Label        string            `json:"label"`
	Min          float64           `json:"min"`
	Max          float64           `json:"max"`
	Step         float64           `json:"step"`
	DefaultValue float64           `json:"defaultValue"`
	Options      []ParameterOption `json:"options,omitempty"`
}

type Descriptor struct {
	Name        string      `json:"name"`
	Aliases     []string    `json:"aliases,omitempty"`
	Version     int         `json:"version"`
	DisplayName string      `json:"displayName"`
	Description string      `json:"description"`
	Input       Port        `json:"input"`
	Output      OutputPort  `json:"output"`
	Parameters  []Parameter `json:"parameters"`
}

// Catalog 由内置插件提供设置声明，宿主和前端不维护另一套参数范围。
func Catalog() []Descriptor {
	parameter := func(key, label string, value, min, max, step float64) Parameter {
		return Parameter{Key: key, Label: label, DefaultValue: value, Min: min, Max: max, Step: step}
	}
	items := []Descriptor{
		{Name: "gain", DisplayName: "增益", Description: "调整全部声道的音量。", Parameters: []Parameter{
			parameter("gainDB", "增益（dB）", 0, -96, 12, 0.1),
		}},
		{Name: "bypass", DisplayName: "直通", Description: "保持音频不变。", Parameters: []Parameter{}},
		{Name: "equalizer", DisplayName: "均衡器", Description: "调整一个频段；可重复添加形成多段均衡。", Parameters: []Parameter{
			parameter("frequencyHz", "中心频率（Hz）", 1000, 20, 20000, 1),
			parameter("q", "Q 值", 0.707, 0.1, 20, 0.001),
			parameter("gainDB", "增益（dB）", 0, -24, 24, 0.1),
		}},
		{Name: "compressor", DisplayName: "压缩器", Description: "联动全部声道控制动态范围，保持声像。", Parameters: []Parameter{
			parameter("thresholdDB", "阈值（dB）", -18, -60, 0, 0.1),
			parameter("ratio", "压缩比", 4, 1, 20, 0.1),
			parameter("attackMs", "起音（ms）", 10, 0.1, 200, 0.1),
			parameter("releaseMs", "释放（ms）", 100, 10, 3000, 1),
			parameter("makeupDB", "补偿增益（dB）", 0, 0, 24, 0.1),
			parameter("kneeDB", "软拐点（dB）", 6, 0, 24, 0.1),
		}},
		{Name: "reverb", DisplayName: "混响", Description: "添加空间余响，低频效果声道保持直通。", Parameters: []Parameter{
			parameter("roomSize", "房间尺寸", 0.5, 0, 1, 0.01),
			parameter("damping", "阻尼", 0.5, 0, 1, 0.01),
			parameter("wet", "混响比例", 0.2, 0, 1, 0.01),
		}},
		{Name: "channelmix", DisplayName: "声道转换", Description: "转换中间声道布局；扩展声道不合成环绕声。", Parameters: []Parameter{
			{Key: "outputChannels", Label: "输出声道", DefaultValue: 2, Min: 1, Max: 8, Step: 1,
				Options: []ParameterOption{{"单声道", 1}, {"立体声", 2}, {"5.1", 6}, {"7.1", 8}}},
		}},
	}
	for i := range items {
		items[i].Version = 1
		items[i].Input = Port{SampleFormat: "f32le", SampleRate: 48000, Layouts: []string{"mono", "stereo", "5.1", "7.1"}}
		items[i].Output = OutputPort{CanContinueDSP: true, SameAsInput: items[i].Name != "channelmix", Layouts: []string{"mono", "stereo", "5.1", "7.1"}}
		if items[i].Name != "bypass" && items[i].Name != "gain" {
			items[i].Aliases = []string{"musicfs.dsp." + items[i].Name}
		}
		if items[i].Name == "channelmix" {
			items[i].Aliases = append(items[i].Aliases, "musicfs.plugin.decoder.channelmix")
		}
	}
	return items
}

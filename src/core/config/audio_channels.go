package config

import "fmt"

// AudioChannelsConfig 是声道转换插件的启动配置，影响编码与客户端协商。
type AudioChannelsConfig struct {
	OutputLayout string `yaml:"outputLayout,omitempty" json:"outputLayout,omitempty"`
}

func (c AudioChannelsConfig) Validate() error {
	switch c.OutputLayout {
	case "", "stereo", "5.1", "7.1":
		return nil
	default:
		return fmt.Errorf("audioChannels.outputLayout must be stereo, 5.1 or 7.1")
	}
}

func (c AudioChannelsConfig) RequiresProcessing() bool {
	return c.OutputLayout == "5.1" || c.OutputLayout == "7.1"
}

func audioChannelsShape() fieldShape {
	return mappingShape(map[string]fieldShape{"outputLayout": stringShape})
}

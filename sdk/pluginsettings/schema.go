// Package pluginsettings 声明由宿主安全渲染的插件设置页面，不执行插件脚本或外部 URL。
package pluginsettings

type Option struct {
	Value           string            `json:"value"`
	Label           string            `json:"label"`
	LocalizedLabels map[string]string `json:"localizedLabels,omitempty"`
}

type Field struct {
	LocalizedLabels       map[string]string `json:"localizedLabels,omitempty"`
	LocalizedDescriptions map[string]string `json:"localizedDescriptions,omitempty"`
	Key                   string            `json:"key"`
	Type                  string            `json:"type"`
	Label                 string            `json:"label"`
	Description           string            `json:"description"`
	DefaultValue          any               `json:"defaultValue"`
	Min                   *float64          `json:"min,omitempty"`
	Max                   *float64          `json:"max,omitempty"`
	Options               []Option          `json:"options,omitempty"`
}

type Action struct {
	LocalizedLabels       map[string]string `json:"localizedLabels,omitempty"`
	LocalizedDescriptions map[string]string `json:"localizedDescriptions,omitempty"`
	ID                    string            `json:"id"`
	Label                 string            `json:"label"`
	Description           string            `json:"description"`
}

type Schema struct {
	LocalizedTitles       map[string]string `json:"localizedTitles,omitempty"`
	LocalizedDescriptions map[string]string `json:"localizedDescriptions,omitempty"`
	Revision              string            `json:"revision"`
	Title                 string            `json:"title"`
	Description           string            `json:"description"`
	Fields                []Field           `json:"fields"`
	Actions               []Action          `json:"actions"`
}

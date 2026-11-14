// Package contenttype 定义由宿主校验和保存的内容类型声明，不执行插件界面脚本。
package contenttype

const (
	ViewWorkList   = "work-list"
	ViewWorkDetail = "work-detail"
)

// NamedValue 使用稳定标识符关联数据，名称只用于显示，不参与身份判断。
type NamedValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Library 声明独立作品库的导航位置、宿主图标以及作者可选择的曲目展示能力。
type Library struct {
	Order             int    `json:"order"`
	Icon              string `json:"icon"`
	TrackListOptional bool   `json:"trackListOptional"`
}

// Descriptor 描述内容类型的署名角色、外部编号来源和受支持的宿主视图。
// 外部编号来源只声明命名空间，不包含编号值校验、目录识别或数据导入能力。
// Views 只引用宿主提供的固定视图，不代表这些视图已经完成实现。
type Descriptor struct {
	ID                   string       `json:"id"`
	PluginID             string       `json:"pluginId"`
	PluginVersion        string       `json:"pluginVersion"`
	Name                 string       `json:"name"`
	Description          string       `json:"description"`
	CreditRoles          []NamedValue `json:"creditRoles"`
	ExternalIDNamespaces []NamedValue `json:"externalIdNamespaces"`
	Views                []string     `json:"views"`
	Library              *Library     `json:"library,omitempty"`
}

// Clone 返回独立副本，调用方修改集合时不会改变原始声明；空集合统一为非空切片。
func (d Descriptor) Clone() Descriptor {
	if d.Library != nil {
		library := *d.Library
		d.Library = &library
	}
	d.CreditRoles = append([]NamedValue{}, d.CreditRoles...)
	d.ExternalIDNamespaces = append([]NamedValue{}, d.ExternalIDNamespaces...)
	d.Views = append([]string{}, d.Views...)
	return d
}

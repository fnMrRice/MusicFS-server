package tagformat

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// Descriptor 声明插件支持的格式、扩展名和可写字段；宿主保留路径安全检查。
type Descriptor struct {
	Formats    []string
	Extensions []string
	Fields     []string
}

func (d Descriptor) Validate() error {
	if len(d.Formats) == 0 || len(d.Extensions) == 0 || len(d.Fields) == 0 || len(d.Formats) > 32 || len(d.Extensions) > 32 || len(d.Fields) > 32 {
		return fmt.Errorf("INVALID_ARGUMENT: invalid tag format descriptor")
	}
	for _, list := range [][]string{d.Formats, d.Extensions, d.Fields} {
		for i, item := range list {
			if item == "" || len(item) > 128 || strings.ContainsAny(item, "\\/:\x00\r\n ") || slices.Contains(list[:i], item) {
				return fmt.Errorf("INVALID_ARGUMENT: invalid tag format capability")
			}
		}
	}
	for _, extension := range d.Extensions {
		if len(extension) < 2 || extension[0] != '.' || strings.Contains(extension[1:], ".") || extension != strings.ToLower(extension) {
			return fmt.Errorf("INVALID_ARGUMENT: invalid tag format extension")
		}
	}
	return nil
}

func (d Descriptor) SupportsName(name string) bool {
	return slices.Contains(d.Extensions, strings.ToLower(path.Ext(name)))
}

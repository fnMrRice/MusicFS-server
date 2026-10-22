// Package contenttypes 管理已校验的内容类型声明，具体类型由装配层注入。
package contenttypes

import (
	"fmt"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
)

// Registry 创建后不可变，可由多个请求并发读取。
type Registry struct {
	descriptors []contenttype.Descriptor
	byID        map[string]int
}

// New 按传入顺序注册内容类型；重复类型或重复插件身份均视为装配错误。
func New(descriptors ...contenttype.Descriptor) (*Registry, error) {
	r := &Registry{
		descriptors: make([]contenttype.Descriptor, 0, len(descriptors)),
		byID:        make(map[string]int, len(descriptors)),
	}
	plugins := make(map[string]bool, len(descriptors))
	for _, descriptor := range descriptors {
		if err := descriptor.Validate(); err != nil {
			return nil, err
		}
		if _, exists := r.byID[descriptor.ID]; exists {
			return nil, fmt.Errorf("INVALID_ARGUMENT: duplicate content type id")
		}
		if plugins[descriptor.PluginID] {
			return nil, fmt.Errorf("INVALID_ARGUMENT: duplicate content type plugin id")
		}
		r.byID[descriptor.ID] = len(r.descriptors)
		plugins[descriptor.PluginID] = true
		r.descriptors = append(r.descriptors, descriptor.Clone())
	}
	return r, nil
}

// List 返回按注册顺序排列的独立声明副本，空注册表返回空数组。
func (r *Registry) List() []contenttype.Descriptor {
	result := []contenttype.Descriptor{}
	if r == nil {
		return result
	}
	for _, descriptor := range r.descriptors {
		result = append(result, descriptor.Clone())
	}
	return result
}

// Get 按稳定类型标识符查询，返回值的修改不会改变注册表。
func (r *Registry) Get(id string) (contenttype.Descriptor, bool) {
	if r == nil {
		return contenttype.Descriptor{}, false
	}
	index, ok := r.byID[id]
	if !ok {
		return contenttype.Descriptor{}, false
	}
	return r.descriptors[index].Clone(), true
}

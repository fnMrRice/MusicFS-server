package audio

import (
	"fmt"
	"sync"
)

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
	Name          string      `json:"name"`
	Aliases       []string    `json:"aliases,omitempty"`
	Version       int         `json:"version"`
	DisplayName   string      `json:"displayName"`
	Description   string      `json:"description"`
	Input         Port        `json:"input"`
	Output        OutputPort  `json:"output"`
	Parameters    []Parameter `json:"parameters"`
	PluginID      string      `json:"-"`
	PluginVersion string      `json:"-"`
	Capability    string      `json:"-"`
}
type definition struct {
	descriptor Descriptor
	factory    Factory
}

var definitions = struct {
	sync.RWMutex
	items []definition
	names map[string]int
}{names: map[string]int{}}

// RegisterDefinition 由装配层注册参数声明和节点工厂，宿主按能力处理策略。
func RegisterDefinition(d Descriptor, factory Factory) error {
	definitions.Lock()
	defer definitions.Unlock()
	if d.Name == "" || d.Version != NodeVersion || factory == nil {
		return fmt.Errorf("invalid DSP definition")
	}
	names := append([]string{d.Name}, d.Aliases...)
	seen := map[string]bool{}
	for _, name := range names {
		if _, ok := definitions.names[name]; ok || name == "" || seen[name] {
			return fmt.Errorf("duplicate DSP definition")
		}
		seen[name] = true
	}
	index := len(definitions.items)
	definitions.items = append(definitions.items, definition{cloneDescriptor(d), factory})
	for _, name := range names {
		definitions.names[name] = index
	}
	return nil
}
func cloneDescriptor(d Descriptor) Descriptor {
	d.Aliases = append([]string(nil), d.Aliases...)
	d.Input.Layouts = append([]string{}, d.Input.Layouts...)
	d.Output.Layouts = append([]string{}, d.Output.Layouts...)
	d.Parameters = append([]Parameter{}, d.Parameters...)
	for i := range d.Parameters {
		d.Parameters[i].Options = append([]ParameterOption(nil), d.Parameters[i].Options...)
	}
	return d
}
func Catalog() []Descriptor {
	definitions.RLock()
	defer definitions.RUnlock()
	result := make([]Descriptor, 0, len(definitions.items))
	for _, d := range definitions.items {
		result = append(result, cloneDescriptor(d.descriptor))
	}
	return result
}
func Definition(name string) (Descriptor, bool) {
	definitions.RLock()
	defer definitions.RUnlock()
	i, ok := definitions.names[name]
	if !ok {
		return Descriptor{}, false
	}
	return cloneDescriptor(definitions.items[i].descriptor), true
}
func HasCapability(name, capability string) bool {
	d, ok := Definition(name)
	return ok && d.Capability == capability
}
func RegisterDefinitions(r *Registry) error {
	definitions.RLock()
	defer definitions.RUnlock()
	for _, item := range definitions.items {
		for _, name := range append([]string{item.descriptor.Name}, item.descriptor.Aliases...) {
			if err := r.Register(name, item.factory); err != nil {
				return err
			}
		}
	}
	return nil
}

package audio

import "fmt"

// Node 在预览时只描述输出，配置成功后按协商的格式处理音频块。
type Node interface {
	DescribeIO(Format) (Output, error)
	ConfigureIO(Format) (Output, error)
	Process(*Block) error
	Reset() error
	Drain() error
	Close() error
	DelayFrames() int64
}
type Factory func(map[string]float64) (Node, error)
type Registry struct{ factories map[string]Factory }

const NodeVersion = 1

type NodeSpec struct {
	Name    string             `json:"name" yaml:"name"`
	Version int                `json:"version" yaml:"version"`
	Params  map[string]float64 `json:"params,omitempty" yaml:"params,omitempty"`
}

func NewRegistry() *Registry { return &Registry{factories: map[string]Factory{}} }

// Factory 返回已注册工厂，供可信装配复用插件原有参数校验。
func (r *Registry) Factory(name string) (Factory, bool) {
	factory, ok := r.factories[name]
	return factory, ok
}
func (r *Registry) Register(name string, f Factory) error {
	if name == "" || f == nil {
		return fmt.Errorf("invalid node registration")
	}
	if _, ok := r.factories[name]; ok {
		return fmt.Errorf("duplicate node registration")
	}
	r.factories[name] = f
	return nil
}

// Replace 用于已校验的实例配置覆盖默认工厂，变更仅影响当前处理链。
func (r *Registry) Replace(name string, f Factory) error {
	if name == "" || f == nil {
		return fmt.Errorf("invalid node registration")
	}
	r.factories[name] = f
	return nil
}
func (r *Registry) Create(name string, params map[string]float64) (Node, error) {
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("UNSUPPORTED: unknown DSP node")
	}
	return createLoggedNode(name, f, params)
}

func (r *Registry) CreateVersioned(spec NodeSpec) (Node, error) {
	if spec.Version != NodeVersion {
		return nil, fmt.Errorf("UNSUPPORTED: unknown DSP node version")
	}
	return r.Create(spec.Name, spec.Params)
}

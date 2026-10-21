package audio

import "fmt"

type Node interface {
	Configure(Format) error
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

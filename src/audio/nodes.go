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
	return f(params)
}

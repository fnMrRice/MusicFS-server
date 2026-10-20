package onlinehost

import (
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// Retain the last verified identity while an instance is stopped or replaced.
// A never-started instance only has capability policy and no display identity.
func captureDescriptor(r *Runtime, previous pluginproto.OnlineDescriptor) error {
	r.descriptor = previous.Clone()
	if r.Provider == nil || (reflect.ValueOf(r.Provider).Kind() == reflect.Pointer && reflect.ValueOf(r.Provider).IsNil()) {
		return nil
	}
	if provider, ok := r.Provider.(interface {
		Descriptor() pluginproto.OnlineDescriptor
	}); ok {
		descriptor := provider.Descriptor()
		if err := descriptor.Validate(r.Platform); err != nil {
			return err
		}
		r.descriptor = descriptor.Clone()
	}
	return nil
}

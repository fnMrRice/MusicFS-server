package plugindb

import (
	"fmt"
	"sync"
)

type factories struct {
	open OpenFactory
	read ReadFactory
}

var drivers = struct {
	sync.RWMutex
	items map[string]factories
}{items: map[string]factories{}}

// Register 由可信存储驱动注册入口调用；状态存储与主库使用同一后端名称。
func Register(name string, open OpenFactory, read ReadFactory) error {
	if name == "" || open == nil || read == nil {
		return fmt.Errorf("invalid plugin state backend registration")
	}
	drivers.Lock()
	defer drivers.Unlock()
	if _, exists := drivers.items[name]; exists {
		return fmt.Errorf("plugin state backend is already registered")
	}
	drivers.items[name] = factories{open: open, read: read}
	return nil
}

func lookup(name string) (factories, error) {
	if name == "" {
		name = "sqlite"
	}
	drivers.RLock()
	defer drivers.RUnlock()
	result, ok := drivers.items[name]
	if !ok {
		return factories{}, fmt.Errorf("plugin state backend is not registered")
	}
	return result, nil
}

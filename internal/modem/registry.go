package modem

import (
	"fmt"
	"sort"
	"sync"
)

// Factory constructs a Modem from driver-specific Options.
type Factory func(Options) (Modem, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register makes a driver available by name. Driver packages typically call
// this from their init(). Panics if name is empty or already registered, so
// duplicate registrations are caught at startup.
func Register(name string, f Factory) {
	if name == "" {
		panic("modem: Register called with empty name")
	}
	if f == nil {
		panic("modem: Register called with nil factory for " + name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		panic("modem: driver already registered: " + name)
	}
	registry[name] = f
}

// Open constructs the named driver with the given options.
// Returns an error if no driver is registered under that name.
// Open does not call Modem.Open on the returned value; the caller owns lifecycle.
func Open(name string, opts Options) (Modem, error) {
	registryMu.RLock()
	f, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("modem: unknown driver %q (available: %v)", name, Drivers())
	}
	return f(opts)
}

// Drivers returns the sorted list of registered driver names.
func Drivers() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

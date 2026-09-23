package tango

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrDuplicateLifecycle is returned by Registry.RegisterLifecycle when a
// Lifecycle with the same Name has already been registered.
var ErrDuplicateLifecycle = errors.New("tango: duplicate lifecycle name")

// ErrReservedLifecycleName is returned by Registry.RegisterLifecycle when a
// Lifecycle's Name collides with the internal name ServeContext reserves for
// the Scheduler lifecycle it appends itself (see schedulerLifecycleName).
var ErrReservedLifecycleName = errors.New("tango: reserved lifecycle name")

// Lifecycle is a named component with optional startup and shutdown hooks,
// run by ServeContext around the HTTP server's own lifetime. Start and Stop
// are each optional (nil is skipped), but at least one of them must be set.
type Lifecycle struct {
	Name  string
	Start func(context.Context) error
	Stop  func(context.Context) error
}

// RegisterLifecycle adds lifecycle to the registry. It rejects a blank (or
// whitespace-only) Name, rejects a Lifecycle with both Start and Stop nil,
// and returns an error wrapping ErrDuplicateLifecycle if a Lifecycle with
// the same Name has already been registered.
func (r *Registry) RegisterLifecycle(lifecycle Lifecycle) error {
	name := strings.TrimSpace(lifecycle.Name)
	if name == "" {
		return fmt.Errorf("tango: lifecycle name must not be blank")
	}
	if lifecycle.Start == nil && lifecycle.Stop == nil {
		return fmt.Errorf("tango: lifecycle %q must set Start, Stop, or both", lifecycle.Name)
	}
	if lifecycle.Name == schedulerLifecycleName {
		return fmt.Errorf("%w: %q", ErrReservedLifecycleName, lifecycle.Name)
	}

	if _, exists := r.lifecycleNames[lifecycle.Name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateLifecycle, lifecycle.Name)
	}

	if r.lifecycleNames == nil {
		r.lifecycleNames = make(map[string]struct{})
	}
	r.lifecycleNames[lifecycle.Name] = struct{}{}
	r.lifecycles = append(r.lifecycles, lifecycle)

	return nil
}

// Lifecycles returns every registered Lifecycle in registration order.
func (r *Registry) Lifecycles() []Lifecycle {
	return r.lifecycles
}

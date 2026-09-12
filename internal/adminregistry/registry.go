package adminregistry

import (
	"fmt"
	"reflect"

	"github.com/angvp/tango/model"
)

// Options controls how a model appears in the admin.
type Options struct {
	ListDisplay []string
	Search      []string
	Ordering    []string
}

// ModelRegistration is the admin metadata for a registered model.
type ModelRegistration struct {
	Model   model.ModelMeta
	Options Options
}

// Registry stores admin registrations by model name.
type Registry struct {
	models        *model.Registry
	registrations map[string]ModelRegistration
}

// NewRegistry returns an empty, ready-to-use admin Registry.
func NewRegistry(models *model.Registry) *Registry {
	return &Registry{
		models:        models,
		registrations: make(map[string]ModelRegistration),
	}
}

// Register stores admin presentation options for a model.
func (r *Registry) Register(value any, opts Options) error {
	name := modelName(value)

	meta, ok := r.models.Get(name)
	if !ok {
		if err := r.models.Register(value); err != nil {
			return err
		}
		meta, ok = r.models.Get(name)
		if !ok {
			return fmt.Errorf("tango admin: registered model %q not found", name)
		}
	}

	if err := validateFields(meta, opts.ListDisplay, "ListDisplay"); err != nil {
		return err
	}
	if err := validateFields(meta, opts.Search, "Search"); err != nil {
		return err
	}
	if err := validateFields(meta, opts.Ordering, "Ordering"); err != nil {
		return err
	}

	r.registrations[name] = ModelRegistration{
		Model:   meta,
		Options: opts,
	}

	return nil
}

// Get returns an admin registration by model name.
func (r *Registry) Get(name string) (ModelRegistration, bool) {
	registration, ok := r.registrations[name]
	return registration, ok
}

// Registrations returns every registered model in unspecified order.
func (r *Registry) Registrations() []ModelRegistration {
	registrations := make([]ModelRegistration, 0, len(r.registrations))
	for _, registration := range r.registrations {
		registrations = append(registrations, registration)
	}
	return registrations
}

func validateFields(meta model.ModelMeta, names []string, option string) error {
	fields := make(map[string]struct{}, len(meta.Fields))
	for _, field := range meta.Fields {
		fields[field.Name] = struct{}{}
	}

	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("tango admin: %s references unknown field %q on %s", option, name, meta.Name)
		}
	}

	return nil
}

func modelName(value any) string {
	modelType := reflect.TypeOf(value)
	if modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
	}
	return modelType.Name()
}

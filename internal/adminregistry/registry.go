package adminregistry

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/angvp/tango/model"
)

// Options controls how a model appears in the admin.
type Options struct {
	ListDisplay []string
	Search      []string
	Ordering    []string
	// Label names one of this model's own fields to display whenever it is
	// shown as a related object elsewhere (e.g. on a Post's admin page for
	// a Post.AuthorID foreign key). Unset falls back to showing the raw
	// primary key value — never a hard error.
	Label string
	// Labels overrides a field's humanized default display label, keyed by
	// Go field name.
	Labels map[string]string
	// HelpText adds descriptive text below a field's form control, keyed by
	// Go field name.
	HelpText map[string]string
	// ReadOnly lists fields that render non-editably in create/edit forms.
	// On edit, a read-only field keeps its existing stored value regardless
	// of submitted form data; on create, it stays at its Go zero value.
	// Submitted data for a read-only field is always ignored, never parsed.
	ReadOnly []string
	// FieldOrder lists fields in the order they should render; fields not
	// listed keep their existing default order after the ordered ones.
	FieldOrder []string
	// Widgets overrides how a field renders and parses in create/edit
	// forms, keyed by Go field name. See Widget — best-effort, not a
	// stable v0.1 contract.
	Widgets map[string]Widget
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
	if err := validateOrderingFields(meta, opts.Ordering); err != nil {
		return err
	}
	if opts.Label != "" {
		if err := validateFields(meta, []string{opts.Label}, "Label"); err != nil {
			return err
		}
	}
	if err := validateFieldMapKeys(meta, opts.Labels, "Labels"); err != nil {
		return err
	}
	if err := validateFieldMapKeys(meta, opts.HelpText, "HelpText"); err != nil {
		return err
	}
	if err := validateFields(meta, opts.ReadOnly, "ReadOnly"); err != nil {
		return err
	}
	if err := validateFields(meta, opts.FieldOrder, "FieldOrder"); err != nil {
		return err
	}
	if err := validateFieldMapKeys(meta, opts.Widgets, "Widgets"); err != nil {
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

// validateFieldMapKeys validates the keys of a field-name-keyed option map
// (Labels, HelpText, Widgets), reusing validateFields' unknown-field error.
func validateFieldMapKeys[V any](meta model.ModelMeta, m map[string]V, option string) error {
	if len(m) == 0 {
		return nil
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return validateFields(meta, names, option)
}

// validateOrderingFields validates Ordering the same way validateFields
// validates every other field-name option, except it strips a leading "-"
// (the db.Query.OrderBy descending-order convention) before checking the
// name against the model's fields.
func validateOrderingFields(meta model.ModelMeta, names []string) error {
	stripped := make([]string, len(names))
	for i, name := range names {
		stripped[i] = strings.TrimPrefix(name, "-")
	}
	return validateFields(meta, stripped, "Ordering")
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

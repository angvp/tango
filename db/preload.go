package db

import (
	"context"
	"fmt"
	"reflect"

	"github.com/angvp/tango/model"
)

// preloadPlan is one relation named in Query.Preload, resolved against the
// model's declared relations and the Store's registry before any SQL is sent.
type preloadPlan struct {
	relation model.RelationMeta
	target   model.ModelMeta
}

// resolvePreload checks the names in a Query's Preload against meta's declared
// relations and returns them in the order named. An empty request resolves to
// nothing. A name matches only a declared relation field, so a dotted path is
// refused as unknown. Every refusal is a *QueryError raised before any SQL.
func (s *Store) resolvePreload(meta model.ModelMeta, names []string) ([]preloadPlan, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if s.models == nil {
		return nil, &QueryError{Model: meta.Name, Reason: "Preload needs the Store's model registry; call UseModels first"}
	}
	plans := make([]preloadPlan, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			return nil, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("relation %q is named more than once", name)}
		}
		seen[name] = true
		relation, ok := relationNamed(meta, name)
		if !ok {
			return nil, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("unknown relation %q for model %s", name, meta.Name)}
		}
		if relation.Direction != "belongs_to" {
			return nil, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("relation %q is %s, which Preload does not load yet", name, relation.Direction)}
		}
		target, ok := s.models.Get(relation.Target)
		if !ok {
			return nil, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("relation %q targets %s, which is not registered", name, relation.Target)}
		}
		plans = append(plans, preloadPlan{relation: relation, target: target})
	}
	return plans, nil
}

// relationNamed returns the declared relation whose field name is name.
func relationNamed(meta model.ModelMeta, name string) (model.RelationMeta, bool) {
	for _, relation := range meta.Relations {
		if relation.Name == name {
			return relation, true
		}
	}
	return model.RelationMeta{}, false
}

// preloadRows loads each planned relation for rows, a slice of meta's structs,
// and sets the relation field of every row. It writes only to rows, which the
// caller owns for the duration of the read, so a failed read leaves the
// caller's destination untouched.
func (s *Store) preloadRows(ctx context.Context, meta model.ModelMeta, rows reflect.Value, plans []preloadPlan) error {
	for _, plan := range plans {
		var err error
		switch plan.relation.Direction {
		case "belongs_to":
			err = s.preloadBelongsTo(ctx, rows, plan)
		default:
			err = &QueryError{Model: meta.Name, Field: plan.relation.Name, Reason: fmt.Sprintf("unsupported relation direction %q", plan.relation.Direction)}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// preloadBelongsTo sets each row's pointer to its related row. A zero foreign
// key, or one with no related row, leaves the pointer nil. Each row gets its
// own copy of the related struct.
func (s *Store) preloadBelongsTo(ctx context.Context, rows reflect.Value, plan preloadPlan) error {
	primaryKey, err := findPrimaryKeyField(plan.target)
	if err != nil {
		return &QueryError{Model: plan.target.Name, Reason: err.Error()}
	}
	loaded := reflect.MakeMap(reflect.MapOf(primaryKey.Type, plan.target.Type))
	if err := s.Related(ctx, rows.Interface(), plan.relation.ForeignKey, plan.target, loaded.Interface()); err != nil {
		return err
	}
	for i := 0; i < rows.Len(); i++ {
		row := rows.Index(i)
		key := row.FieldByName(plan.relation.ForeignKey)
		if key.IsZero() {
			continue
		}
		found := loaded.MapIndex(key)
		if !found.IsValid() {
			continue
		}
		own := reflect.New(plan.target.Type)
		own.Elem().Set(found)
		row.FieldByName(plan.relation.Name).Set(own)
	}
	return nil
}

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/angvp/tango/model"
)

const (
	// MaxPreloadDepth is the most segments one dotted Query.Preload path may
	// have. "Comments.Account" is two segments; a path of four is refused.
	MaxPreloadDepth = 3
	// MaxPreloadPaths is the most distinct relation reads one Query.Preload
	// may request. A dotted path counts once for each of its segments that no
	// earlier name already requested.
	MaxPreloadPaths = 20
)

// preloadNode is one relation read of a Query.Preload request: the relation
// of the model at its level, the model it loads, and the reads a dotted path
// asks for below it.
type preloadNode struct {
	relation model.RelationMeta
	target   model.ModelMeta
	children []*preloadNode
}

// preloadTree is a Query.Preload request resolved against the registry.
// nested reports whether any name is a dotted path, which is what opens the
// read-only transaction the request runs in.
type preloadTree struct {
	roots  []*preloadNode
	nested bool
}

// resolvePreload checks each name in a Query's Preload against the declared
// relations of meta and of each model a dotted path reaches, and returns the
// request as a tree. A name implies its prefixes, so "Comments.Account" alone
// loads Comments too. Every refusal is raised before any SQL: a repeated name,
// an unknown segment, a path deeper than MaxPreloadDepth, or more distinct
// reads than MaxPreloadPaths.
func (s *Store) resolvePreload(meta model.ModelMeta, names []string) (preloadTree, error) {
	if len(names) == 0 {
		return preloadTree{}, nil
	}
	if s.models == nil {
		return preloadTree{}, &QueryError{Model: meta.Name, Reason: "Preload needs the Store's model registry; call UseModels first"}
	}
	var tree preloadTree
	seen := make(map[string]bool, len(names))
	reads := 0
	for _, name := range names {
		if seen[name] {
			return preloadTree{}, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("relation %q is named more than once", name)}
		}
		seen[name] = true
		segments := strings.Split(name, ".")
		if len(segments) > MaxPreloadDepth {
			return preloadTree{}, &QueryLimitError{Limit: limitPreloadDepth, Max: MaxPreloadDepth, Got: len(segments)}
		}
		if len(segments) > 1 {
			tree.nested = true
		}
		level := &tree.roots
		current := meta
		for _, segment := range segments {
			relation, ok := relationNamed(current, segment)
			if !ok {
				return preloadTree{}, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("unknown relation %q in path %q for model %s", segment, name, current.Name)}
			}
			node := findPreloadNode(*level, relation.Name)
			if node == nil {
				target, ok := s.models.Get(relation.Target)
				if !ok {
					return preloadTree{}, &QueryError{Model: meta.Name, Field: name, Reason: fmt.Sprintf("relation %q targets %s, which is not registered", relation.Name, relation.Target)}
				}
				reads++
				if reads > MaxPreloadPaths {
					return preloadTree{}, &QueryLimitError{Limit: limitPreloadPaths, Max: MaxPreloadPaths, Got: reads}
				}
				node = &preloadNode{relation: relation, target: target}
				*level = append(*level, node)
			}
			level = &node.children
			current = node.target
		}
	}
	return tree, nil
}

// findPreloadNode returns the node of nodes for the relation named name, or
// nil when no request has reached it yet.
func findPreloadNode(nodes []*preloadNode, name string) *preloadNode {
	for _, node := range nodes {
		if node.relation.Name == name {
			return node
		}
	}
	return nil
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

// preloadTxOptions is the transaction a request with a dotted path reads in:
// one read-only REPEATABLE READ view, so every level sees the same committed
// data. A Store already bound to a transaction joins it instead.
var preloadTxOptions = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}

// withPreloadTx runs read on the Store it is given. A request with a dotted
// path, made on a Store not already bound to a transaction, runs read in one
// transaction of its own and commits it when read succeeds. Any other request
// runs read directly, with no transaction.
func (s *Store) withPreloadTx(ctx context.Context, tree preloadTree, read func(*Store) error) (err error) {
	if !tree.nested || s.tx != nil {
		return read(s)
	}
	tx, err := s.db.BeginTx(ctx, preloadTxOptions)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// sql.ErrTxDone means the transaction already ended, as it does after
		// a cancelled context; the read error is then the only failure.
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("roll back preload read: %w", rbErr))
		}
	}()
	if err := read(&Store{db: s.db, tx: tx, dialect: s.dialect, models: s.models}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// preloadRows loads each node's relation for rows, a slice of meta's structs,
// sets the relation field of every row, and then loads the nested reads of
// each node on the related rows it returned. It writes only to rows, which the
// caller owns for the duration of the read, so a failed read leaves the
// caller's destination untouched.
func (s *Store) preloadRows(ctx context.Context, meta model.ModelMeta, rows reflect.Value, nodes []*preloadNode) error {
	if rows.Len() == 0 {
		return nil
	}
	for _, node := range nodes {
		var related reflect.Value
		var err error
		switch node.relation.Direction {
		case "belongs_to":
			related, err = s.preloadBelongsTo(ctx, rows, node)
		case "has_many":
			related, err = s.preloadHasMany(ctx, meta, rows, node)
		default:
			err = &QueryError{Model: meta.Name, Field: node.relation.Name, Reason: fmt.Sprintf("unsupported relation direction %q", node.relation.Direction)}
		}
		if err != nil {
			return err
		}
		if err := s.preloadRows(ctx, node.target, related, node.children); err != nil {
			return err
		}
	}
	return nil
}

// preloadBelongsTo sets each row's pointer to its related row and returns
// those related rows as one slice, in row order. A zero foreign key, or one
// with no related row, leaves the pointer nil. Each row gets its own element
// in the slice, so rows sharing a key do not share a struct, and the pointer
// points into that slice so a nested read on the slice fills the row too.
func (s *Store) preloadBelongsTo(ctx context.Context, rows reflect.Value, node *preloadNode) (reflect.Value, error) {
	primaryKey, err := findPrimaryKeyField(node.target)
	if err != nil {
		return reflect.Value{}, &QueryError{Model: node.target.Name, Reason: err.Error()}
	}
	loaded := reflect.MakeMap(reflect.MapOf(primaryKey.Type, node.target.Type))
	if err := s.Related(ctx, rows.Interface(), node.relation.ForeignKey, node.target, loaded.Interface()); err != nil {
		return reflect.Value{}, err
	}
	matched := make([]int, 0, rows.Len())
	for i := 0; i < rows.Len(); i++ {
		key := rows.Index(i).FieldByName(node.relation.ForeignKey)
		if key.IsZero() || !loaded.MapIndex(key).IsValid() {
			continue
		}
		matched = append(matched, i)
	}
	related := reflect.MakeSlice(reflect.SliceOf(node.target.Type), len(matched), len(matched))
	for k, i := range matched {
		row := rows.Index(i)
		related.Index(k).Set(loaded.MapIndex(row.FieldByName(node.relation.ForeignKey)))
		row.FieldByName(node.relation.Name).Set(related.Index(k).Addr())
	}
	return related, nil
}

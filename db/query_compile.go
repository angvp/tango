package db

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/angvp/tango/model"
)

// whereClause validates query's Where, Any and Filter against meta and
// renders them as one WHERE clause: (Where-AND) AND (Any-OR) AND (Filter),
// each part only when present, with placeholders numbered in that order. It
// never modifies query. List, Count and every later read share it.
func (s *Store) whereClause(meta model.ModelMeta, query Query) (string, []any, error) {
	for _, group := range [][]Condition{query.Where, query.Any} {
		for _, condition := range group {
			if _, err := validateCondition(meta, condition, "Where "); err != nil {
				return "", nil, legacyQueryError(meta, condition.Field, err)
			}
		}
	}
	if query.Filter != nil {
		if err := validateFilter(meta, *query.Filter); err != nil {
			return "", nil, err
		}
	}

	var groups []string
	var args []any
	for _, conditions := range []struct {
		items []Condition
		join  string
	}{{query.Where, " AND "}, {query.Any, " OR "}} {
		if len(conditions.items) == 0 {
			continue
		}
		clauses := make([]string, len(conditions.items))
		for i, condition := range conditions.items {
			clauses[i] = s.renderCondition(condition, &args)
		}
		groups = append(groups, "("+strings.Join(clauses, conditions.join)+")")
	}
	if query.Filter != nil {
		groups = append(groups, s.renderFilter(*query.Filter, &args))
	}
	if len(groups) == 0 {
		return "", nil, nil
	}
	return " WHERE " + strings.Join(groups, " AND "), args, nil
}

// renderCondition renders one validated condition, appending its bind
// argument to args.
func (s *Store) renderCondition(condition Condition, args *[]any) string {
	if isNullOp(condition.Op) {
		return s.ident(condition.Field) + " " + string(condition.Op)
	}
	clause := s.ident(condition.Field) + " " + string(condition.Op) + " " + placeholder(s.dialect, len(*args)+1)
	*args = append(*args, argumentValue(s.dialect, condition.Value))
	return clause
}

// renderFilter renders a validated Filter tree as one parenthesized
// expression.
func (s *Store) renderFilter(expr Expr, args *[]any) string {
	rendered := s.renderExpr(expr, args)
	if expr.Op == ExprCond || expr.Op == ExprNot {
		return "(" + rendered + ")"
	}
	return rendered
}

func (s *Store) renderExpr(expr Expr, args *[]any) string {
	switch expr.Op {
	case ExprCond:
		return s.renderCondition(expr.Cond, args)
	case ExprNot:
		return "NOT (" + s.renderExpr(expr.Children[0], args) + ")"
	}
	join := " AND "
	if expr.Op == ExprOr {
		join = " OR "
	}
	parts := make([]string, len(expr.Children))
	for i, child := range expr.Children {
		parts[i] = s.renderExpr(child, args)
	}
	return "(" + strings.Join(parts, join) + ")"
}

// validateFilter checks the shape limits first (so a hostile or cyclic tree
// is refused without being walked in full), then the structure and every leaf.
func validateFilter(meta model.ModelMeta, root Expr) error {
	nodes := 0
	if err := checkFilterLimits(root, 1, &nodes); err != nil {
		return err
	}
	return checkFilterNode(meta, root)
}

// checkFilterLimits stops at the first node or level over the limit.
func checkFilterLimits(expr Expr, depth int, nodes *int) error {
	if depth > MaxFilterDepth {
		return &QueryLimitError{Limit: limitFilterDepth, Max: MaxFilterDepth, Got: depth}
	}
	*nodes++
	if *nodes > MaxFilterNodes {
		return &QueryLimitError{Limit: limitFilterNodes, Max: MaxFilterNodes, Got: *nodes}
	}
	for _, child := range expr.Children {
		if err := checkFilterLimits(child, depth+1, nodes); err != nil {
			return err
		}
	}
	return nil
}

func checkFilterNode(meta model.ModelMeta, expr Expr) error {
	invalid := func(format string, args ...any) error {
		return &QueryError{Model: meta.Name, Reason: fmt.Sprintf(format, args...)}
	}
	switch expr.Op {
	case "":
		return invalid("expression has no operator")
	case ExprCond:
		if len(expr.Children) > 0 {
			return invalid("condition has children")
		}
		if _, err := validateCondition(meta, expr.Cond, ""); err != nil {
			return &QueryError{Model: meta.Name, Field: expr.Cond.Field, Reason: err.Error()}
		}
		return nil
	case ExprAnd, ExprOr, ExprNot:
		if expr.Cond.Field != "" || expr.Cond.Op != "" || expr.Cond.Value != nil {
			return invalid("%s group carries a condition", expr.Op)
		}
		if expr.Op == ExprNot && len(expr.Children) != 1 {
			return invalid("NOT needs exactly one child, got %d", len(expr.Children))
		}
		if len(expr.Children) == 0 {
			return invalid("%s group has no children", expr.Op)
		}
		for _, child := range expr.Children {
			if err := checkFilterNode(meta, child); err != nil {
				return err
			}
		}
		return nil
	}
	return invalid("unknown expression operator %q", string(expr.Op))
}

// legacyQueryError wraps a Where or Any validation error so it matches
// ErrInvalidQuery while keeping its text exactly as it always read.
func legacyQueryError(meta model.ModelMeta, field string, err error) error {
	return &QueryError{Model: meta.Name, Field: field, text: err.Error(), Reason: err.Error()}
}

// validateCondition checks condition against meta. label is "Where " for
// the Where and Any lists, whose messages predate Filter, and "" for Filter
// leaves.
func validateCondition(meta model.ModelMeta, condition Condition, label string) (model.FieldMeta, error) {
	var field model.FieldMeta
	known := false
	for _, candidate := range meta.Fields {
		if candidate.Name == condition.Field {
			field = candidate
			known = true
			break
		}
	}
	if !known {
		return field, conditionError(label, "unknown %sfield %q for model %s", label, condition.Field, meta.Name)
	}
	return validateKnownCondition(field, condition, label)
}

// conditionError formats a condition problem. Where and Any messages keep the
// "tango db: " prefix they always had; a Filter leaf's reason is wrapped by
// QueryError, which adds its own.
func conditionError(label, format string, args ...any) error {
	if label != "" {
		format = "tango db: " + format
	}
	return fmt.Errorf(format, args...)
}

// isNullOp reports whether op tests for NULL and so has no bind argument.
func isNullOp(op Op) bool { return op == OpIsNull || op == OpIsNotNull }

func validateKnownCondition(field model.FieldMeta, condition Condition, label string) (model.FieldMeta, error) {
	fail := func(format string, args ...any) error { return conditionError(label, format, args...) }

	switch condition.Op {
	case OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpLike:
	case OpIsNull, OpIsNotNull:
		if condition.Value != nil {
			return field, fail("operator %q takes no value (field %q)", condition.Op, field.Name)
		}
		return field, nil
	default:
		return field, fail("unsupported %soperator %q", label, condition.Op)
	}

	fieldKind := field.Type.Kind()
	if condition.Op == OpLike && fieldKind != reflect.String {
		return field, fail("%soperator %q is only supported for string fields, got %q", label, condition.Op, field.Name)
	}
	if condition.Op != OpEq && condition.Op != OpNe && condition.Op != OpLike && field.Type != timeType {
		switch fieldKind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
		default:
			return field, fail("%soperator %q is not supported for field %q of kind %s", label, condition.Op, field.Name, fieldKind)
		}
	}

	if condition.Value == nil {
		if label == "" {
			return field, fail("operator %q needs a value (field %q)", condition.Op, field.Name)
		}
		return field, fail("nil Where value for field %q is not supported", field.Name)
	}
	valueType := reflect.TypeOf(condition.Value)
	if valueType.Kind() != fieldKind || (fieldKind == reflect.Struct && valueType != field.Type) {
		return field, fail("%svalue for field %q has type %s, want %s", label, field.Name, valueType, field.Type)
	}
	return field, nil
}

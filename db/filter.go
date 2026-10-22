package db

import (
	"errors"
	"fmt"
)

// ErrInvalidQuery is matched (with errors.Is) by every error that says a
// Query is malformed: an unknown field, an unsupported operator, a bad value,
// a malformed filter tree, or a filter over a limit. Use errors.As with
// *QueryError or *QueryLimitError for the details.
var ErrInvalidQuery = errors.New("tango db: invalid query")

// ErrQueryLimit is matched (with errors.Is) by the error a Query returns when
// it is over one of the documented limits. Such an error also matches
// ErrInvalidQuery.
var ErrQueryLimit = errors.New("tango db: query limit exceeded")

const (
	// MaxFilterNodes is the most nodes a Query.Filter may hold, counting every
	// operator and every condition.
	MaxFilterNodes = 100
	// MaxFilterDepth is the deepest a Query.Filter may nest; the root is
	// level 1.
	MaxFilterDepth = 8
)

// QueryError says why a Query was refused before any SQL ran. It matches
// ErrInvalidQuery.
type QueryError struct {
	Model  string
	Field  string
	Reason string

	text string // full message of an error that predates QueryError
}

func (e *QueryError) Error() string {
	if e.text != "" {
		return e.text
	}
	return fmt.Sprintf("%v: %s", ErrInvalidQuery, e.Reason)
}

// Is reports whether target is ErrInvalidQuery.
func (e *QueryError) Is(target error) bool { return target == ErrInvalidQuery }

// QueryLimitError says which limit a Query went over. Limit names it
// ("filter nodes" or "filter depth"), Max is the limit and Got the value
// found. When a walk stops as soon as it sees the limit exceeded, Got is the
// first value over Max rather than the full total. It matches ErrQueryLimit
// and ErrInvalidQuery.
type QueryLimitError struct {
	Limit string
	Max   int
	Got   int
}

func (e *QueryLimitError) Error() string {
	switch e.Limit {
	case limitFilterNodes:
		return fmt.Sprintf("%v: filter has %d nodes, limit %d", ErrInvalidQuery, e.Got, e.Max)
	case limitFilterDepth:
		return fmt.Sprintf("%v: filter is %d levels deep, limit %d", ErrInvalidQuery, e.Got, e.Max)
	}
	return fmt.Sprintf("%v: %s is %d, limit %d", ErrInvalidQuery, e.Limit, e.Got, e.Max)
}

// Is reports whether target is ErrQueryLimit or ErrInvalidQuery.
func (e *QueryLimitError) Is(target error) bool {
	return target == ErrQueryLimit || target == ErrInvalidQuery
}

const (
	limitFilterNodes = "filter nodes"
	limitFilterDepth = "filter depth"
)

// ExprOp says what kind of node an Expr is.
type ExprOp string

const (
	// ExprCond is a leaf holding one Condition.
	ExprCond ExprOp = "COND"
	// ExprAnd holds children that must all match.
	ExprAnd ExprOp = "AND"
	// ExprOr holds children of which at least one must match.
	ExprOr ExprOp = "OR"
	// ExprNot holds exactly one child and negates it.
	ExprNot ExprOp = "NOT"
)

// Expr is one node of a Query.Filter tree: plain data, no builder state. A
// leaf (ExprCond) carries Cond and no children; a group (ExprAnd, ExprOr or
// ExprNot) carries Children and no Cond. The zero Expr is invalid. A group
// with one child means that child; a group with none, or a NOT with other
// than one child, is rejected with ErrInvalidQuery.
type Expr struct {
	Op       ExprOp
	Cond     Condition
	Children []Expr
}

// Cond returns a leaf comparing field with value using op.
func Cond(field string, op Op, value any) Expr {
	return Expr{Op: ExprCond, Cond: Condition{Field: field, Op: op, Value: value}}
}

// And returns a group that matches when every child matches.
func And(children ...Expr) Expr { return Expr{Op: ExprAnd, Children: children} }

// Or returns a group that matches when any child matches.
func Or(children ...Expr) Expr { return Expr{Op: ExprOr, Children: children} }

// Not returns a node that matches when child does not. In SQL this is a
// literal NOT (...), so rows whose compared column is NULL are left out.
func Not(child Expr) Expr { return Expr{Op: ExprNot, Children: []Expr{child}} }

// IsNull returns a leaf matching rows where field is NULL, which is how an
// unset foreign key is stored.
func IsNull(field string) Expr { return Cond(field, OpIsNull, nil) }

// IsNotNull returns a leaf matching rows where field is not NULL.
func IsNotNull(field string) Expr { return Cond(field, OpIsNotNull, nil) }

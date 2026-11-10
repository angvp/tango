package model

import (
	"errors"
	"strings"
	"testing"
)

type relPlainMismatch struct {
	ID        int64 `tango:"pk"`
	AccountID int32 `tango:"fk=relAccount"`
}

type relPostNoFKTag struct {
	ID        int64       `tango:"pk"`
	AccountID int64       `tango:"index"`
	Account   *relAccount `tango:"belongs_to=AccountID"`
}

type relPostFKToOtherModel struct {
	ID        int64       `tango:"pk"`
	AccountID int64       `tango:"fk=relComment"`
	Account   *relAccount `tango:"belongs_to=AccountID"`
}

type relPostMissingFK struct {
	ID      int64       `tango:"pk"`
	Account *relAccount `tango:"belongs_to=NoSuchField"`
}

type relPostKeyMismatch struct {
	ID        int64       `tango:"pk"`
	AccountID int32       `tango:"fk=relAccount"`
	Account   *relAccount `tango:"belongs_to=AccountID"`
}

type relCommentNoFK struct {
	ID     int64 `tango:"pk"`
	PostID int64
}

type relCommentFKToOtherModel struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=relAccount"`
}

type relCommentKeyMismatch struct {
	ID     int64 `tango:"pk"`
	PostID int32 `tango:"fk=relPostHasManyKeyMismatch"`
}

type relPostHasManyNoFK struct {
	ID       int64            `tango:"pk"`
	Comments []relCommentNoFK `tango:"has_many=PostID"`
}

type relPostHasManyFKToOtherModel struct {
	ID       int64                      `tango:"pk"`
	Comments []relCommentFKToOtherModel `tango:"has_many=PostID"`
}

type relPostHasManyMissingFK struct {
	ID       int64        `tango:"pk"`
	Comments []relComment `tango:"has_many=NoSuchField"`
}

type relPostHasManyKeyMismatch struct {
	ID       int64                   `tango:"pk"`
	Comments []relCommentKeyMismatch `tango:"has_many=PostID"`
}

func TestValidateRelationsAcceptsDeclarationsInAnyOrder(t *testing.T) {
	orders := map[string][]any{
		"target before owner":    {relAccount{}, relComment{}, relPost{}},
		"owner before targets":   {relPost{}, relComment{}, relAccount{}},
		"children before parent": {relComment{}, relPost{}, relAccount{}},
	}
	for name, models := range orders {
		t.Run(name, func(t *testing.T) {
			registry := registerAll(t, models...)
			if err := registry.ValidateRelations(); err != nil {
				t.Fatalf("ValidateRelations returned error: %v, want nil", err)
			}
		})
	}
}

func TestValidateRelationsRefusesWrongDeclarations(t *testing.T) {
	cases := []struct {
		name     string
		register []any
		wantErrs []string // every substring the error must contain
	}{
		{
			name:     "belongs_to target is not registered",
			register: []any{relPost{}, relComment{}},
			wantErrs: []string{"relPost", "Account", "relAccount"},
		},
		{
			name:     "has_many target is not registered",
			register: []any{relPost{}, relAccount{}},
			wantErrs: []string{"relPost", "Comments", "relComment"},
		},
		{
			name:     "belongs_to foreign key lacks fk tag",
			register: []any{relPostNoFKTag{}, relAccount{}},
			wantErrs: []string{"relPostNoFKTag", "Account", "AccountID"},
		},
		{
			name:     "belongs_to foreign key names another model",
			register: []any{relPostFKToOtherModel{}, relAccount{}, relComment{}},
			wantErrs: []string{"relPostFKToOtherModel", "Account", "AccountID"},
		},
		{
			name:     "belongs_to names a field the model lacks",
			register: []any{relPostMissingFK{}, relAccount{}},
			wantErrs: []string{"relPostMissingFK", "Account", "NoSuchField"},
		},
		{
			name:     "belongs_to key type differs from target primary key",
			register: []any{relPostKeyMismatch{}, relAccount{}},
			wantErrs: []string{"relPostKeyMismatch", "Account", "AccountID"},
		},
		{
			name:     "has_many child lacks the foreign key field",
			register: []any{relPostHasManyNoFK{}, relCommentNoFK{}},
			wantErrs: []string{"relPostHasManyNoFK", "Comments", "PostID"},
		},
		{
			name:     "has_many child foreign key names another model",
			register: []any{relPostHasManyFKToOtherModel{}, relCommentFKToOtherModel{}, relAccount{}},
			wantErrs: []string{"relPostHasManyFKToOtherModel", "Comments", "PostID"},
		},
		{
			name:     "has_many names a field the child lacks",
			register: []any{relPostHasManyMissingFK{}, relComment{}},
			wantErrs: []string{"relPostHasManyMissingFK", "Comments", "NoSuchField"},
		},
		{
			name:     "has_many key type differs from owner primary key",
			register: []any{relPostHasManyKeyMismatch{}, relCommentKeyMismatch{}},
			wantErrs: []string{"relPostHasManyKeyMismatch", "Comments", "PostID"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := registerAll(t, tc.register...)
			err := registry.ValidateRelations()
			if !errors.Is(err, ErrInvalidRelation) {
				t.Fatalf("ValidateRelations error = %v, want it to wrap ErrInvalidRelation", err)
			}
			for _, substr := range tc.wantErrs {
				if !strings.Contains(err.Error(), substr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), substr)
				}
			}
		})
	}
}

// A plain fk= field keeps today's behaviour: a mismatched key type is not a
// relation declaration, so neither check refuses it.
func TestPlainForeignKeyWithMismatchedKeyTypeStillValidates(t *testing.T) {
	registry := registerAll(t, relPlainMismatch{}, relAccount{})
	if err := registry.ValidateForeignKeys(); err != nil {
		t.Fatalf("ValidateForeignKeys returned error: %v, want nil", err)
	}
	if err := registry.ValidateRelations(); err != nil {
		t.Fatalf("ValidateRelations returned error: %v, want nil", err)
	}
}

func registerAll(t *testing.T, values ...any) *Registry {
	t.Helper()
	registry := NewRegistry()
	for _, value := range values {
		if err := registry.Register(value); err != nil {
			t.Fatalf("Register(%T) returned error: %v", value, err)
		}
	}
	return registry
}

type relPostOK struct {
	ID        int64       `tango:"pk"`
	AccountID int64       `tango:"fk=relAccount"`
	Account   *relAccount `tango:"belongs_to=AccountID"`
}

// shadowedAccountPost returns a model whose belongs_to target is a different
// struct type from the registered relAccount: both are named relAccount, and
// only the registered one is the model.
func shadowedAccountPost() any {
	type relAccount struct {
		ID int64 `tango:"pk"`
	}
	type relShadowPost struct {
		ID        int64       `tango:"pk"`
		AccountID int64       `tango:"fk=relAccount"`
		Account   *relAccount `tango:"belongs_to=AccountID"`
	}
	return relShadowPost{}
}

func TestRelationTargetMustBeTheRegisteredType(t *testing.T) {
	// Control: the registered type itself is accepted as the target.
	control := registerAll(t, relAccount{}, relPostOK{})
	if err := control.ValidateRelations(); err != nil {
		t.Fatalf("control ValidateRelations: %v", err)
	}

	registry := registerAll(t, shadowedAccountPost(), relAccount{})
	err := registry.ValidateRelations()
	if !errors.Is(err, ErrInvalidRelation) {
		t.Fatalf("ValidateRelations error = %v, want it to wrap ErrInvalidRelation", err)
	}
	for _, substr := range []string{"relShadowPost", "Account"} {
		if !strings.Contains(err.Error(), substr) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), substr)
		}
	}
}

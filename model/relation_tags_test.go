package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type relAccount struct {
	ID int64 `tango:"pk"`
}

type relComment struct {
	ID     int64 `tango:"pk"`
	PostID int64 `tango:"fk=relPost"`
}

type relPost struct {
	ID        int64        `tango:"pk"`
	AccountID int64        `tango:"fk=relAccount,index"`
	Account   *relAccount  `tango:"belongs_to=AccountID"`
	Comments  []relComment `tango:"has_many=PostID"`
}

func TestRelationTagsAreRecordedApartFromColumns(t *testing.T) {
	registry := NewRegistry()
	for _, value := range []any{relAccount{}, relComment{}, relPost{}} {
		if err := registry.Register(value); err != nil {
			t.Fatalf("Register(%T) returned error: %v", value, err)
		}
	}

	meta, ok := registry.Get("relPost")
	if !ok {
		t.Fatal("relPost is not registered")
	}

	wantFields := []string{"ID", "AccountID"}
	gotFields := make([]string, 0, len(meta.Fields))
	for _, field := range meta.Fields {
		gotFields = append(gotFields, field.Name)
	}
	if !reflect.DeepEqual(gotFields, wantFields) {
		t.Fatalf("Fields = %v, want %v: relation fields must not be columns", gotFields, wantFields)
	}

	wantRelations := []RelationMeta{
		{
			Name:       "Account",
			Direction:  "belongs_to",
			ForeignKey: "AccountID",
			Target:     "relAccount",
			Type:       reflect.TypeOf((*relAccount)(nil)),
			TargetType: reflect.TypeOf(relAccount{}),
		},
		{
			Name:       "Comments",
			Direction:  "has_many",
			ForeignKey: "PostID",
			Target:     "relComment",
			Type:       reflect.TypeOf([]relComment(nil)),
			TargetType: reflect.TypeOf(relComment{}),
		},
	}
	if !reflect.DeepEqual(meta.Relations, wantRelations) {
		t.Fatalf("Relations = %+v, want %+v", meta.Relations, wantRelations)
	}
}

func TestModelWithoutRelationFieldsHasNoRelations(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(relAccount{}); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	meta, _ := registry.Get("relAccount")
	if len(meta.Relations) != 0 {
		t.Fatalf("Relations = %+v, want none", meta.Relations)
	}
}

type relValueBelongs struct {
	ID        int64      `tango:"pk"`
	AccountID int64      `tango:"fk=relAccount"`
	Account   relAccount `tango:"belongs_to=AccountID"`
}

type relScalarHasMany struct {
	ID     int64 `tango:"pk"`
	Count  int64 `tango:"has_many=PostID"`
	PostID int64 `tango:"fk=relPost"`
}

type relArrayOfStructHasMany struct {
	ID       int64         `tango:"pk"`
	Comments [2]relComment `tango:"has_many=PostID"`
}

type relScalarSliceHasMany struct {
	ID      int64   `tango:"pk"`
	PostIDs []int64 `tango:"has_many=PostID"`
}

type relTimeBelongs struct {
	ID   int64      `tango:"pk"`
	When *time.Time `tango:"belongs_to=WhenID"`
}

type relTimeHasMany struct {
	ID    int64       `tango:"pk"`
	Times []time.Time `tango:"has_many=PostID"`
}

type relBothTags struct {
	ID        int64       `tango:"pk"`
	AccountID int64       `tango:"fk=relAccount"`
	Account   *relAccount `tango:"belongs_to=AccountID,has_many=PostID"`
}

type relWithColumnTag struct {
	ID        int64       `tango:"pk"`
	AccountID int64       `tango:"fk=relAccount"`
	Account   *relAccount `tango:"belongs_to=AccountID,index"`
}

type relWithPrimaryKeyTag struct {
	ID       int64        `tango:"pk"`
	Comments []relComment `tango:"has_many=PostID,pk"`
}

type relEmptyBelongs struct {
	ID      int64       `tango:"pk"`
	Account *relAccount `tango:"belongs_to="`
}

type relBareHasMany struct {
	ID       int64        `tango:"pk"`
	Comments []relComment `tango:"has_many"`
}

func TestRelationTagMisuseIsRefusedWithErrInvalidRelation(t *testing.T) {
	cases := []struct {
		name  string
		value any
		field string
	}{
		{name: "belongs_to on a non-pointer struct", value: relValueBelongs{}, field: "Account"},
		{name: "has_many on a non-slice", value: relScalarHasMany{}, field: "Count"},
		{name: "has_many on an array", value: relArrayOfStructHasMany{}, field: "Comments"},
		{name: "has_many on a slice of non-structs", value: relScalarSliceHasMany{}, field: "PostIDs"},
		{name: "belongs_to on a pointer to time.Time", value: relTimeBelongs{}, field: "When"},
		{name: "has_many on a slice of time.Time", value: relTimeHasMany{}, field: "Times"},
		{name: "both relation tags on one field", value: relBothTags{}, field: "Account"},
		{name: "relation tag with a column tag", value: relWithColumnTag{}, field: "Account"},
		{name: "relation tag with pk", value: relWithPrimaryKeyTag{}, field: "Comments"},
		{name: "belongs_to with an empty value", value: relEmptyBelongs{}, field: "Account"},
		{name: "has_many without a value", value: relBareHasMany{}, field: "Comments"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			err := registry.Register(tc.value)
			if !errors.Is(err, ErrInvalidRelation) {
				t.Fatalf("Register error = %v, want it to wrap ErrInvalidRelation", err)
			}
			modelName := reflect.TypeOf(tc.value).Name()
			for _, want := range []string{modelName, tc.field} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err.Error(), want)
				}
			}
			if _, registered := registry.Get(modelName); registered {
				t.Errorf("%s was registered despite the refused relation", modelName)
			}
		})
	}
}

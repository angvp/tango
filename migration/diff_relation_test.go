package migration

import (
	"testing"

	"github.com/angvp/tango/model"
)

type diffRelAccount struct {
	ID int64 `tango:"pk"`
}

type diffRelPlainPost struct {
	ID        int64 `tango:"pk"`
	AccountID int64 `tango:"fk=diffRelAccount,index"`
}

type diffRelPost struct {
	ID        int64           `tango:"pk"`
	AccountID int64           `tango:"fk=diffRelAccount,index"`
	Account   *diffRelAccount `tango:"belongs_to=AccountID"`
}

// A model that gains only relation fields has the same columns, so no
// migration is generated for it.
func TestDiffModelGainingOnlyRelationFieldsProducesNoMigration(t *testing.T) {
	plain := ModelsFromMeta([]model.ModelMeta{registerDiffModel(t, "docs", diffRelPlainPost{})})[0]
	withRelation := ModelsFromMeta([]model.ModelMeta{registerDiffModel(t, "docs", diffRelPost{})})[0]

	initial := mustDiffModels(t, []Model{plain}, SchemaState{Tables: map[string]TableState{}})
	if len(initial) != 1 {
		t.Fatalf("initial diff has %d migrations, want 1 CreateTable", len(initial))
	}
	state, err := Replay(initial)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// The relation model is compared under the plain model's table name, which
	// is the only thing that differs between the two registrations.
	withRelation.Name = plain.Name
	if migrations := mustDiffModels(t, []Model{withRelation}, state); len(migrations) != 0 {
		t.Fatalf("got %d migrations for a model that gained only relation fields, want 0: %+v", len(migrations), migrations)
	}

	withRelation.Columns = append(withRelation.Columns, Column{Name: "title", Type: "text"})
	if migrations := mustDiffModels(t, []Model{withRelation}, state); len(migrations) == 0 {
		t.Fatal("known positive failed: a real added column produced no migration")
	}
}

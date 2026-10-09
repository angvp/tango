package db_test

import (
	"context"
	"sync"
	"testing"

	"github.com/angvp/tango/db"
)

type Ticket struct {
	ID    int64 `tango:"pk"`
	Title string
}

func TestCreateWithoutIDAfterExplicitIDGetsAHigherID(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, Ticket{})
	meta, _ := registry.Get("Ticket")
	store := db.NewStore(sqlDB, dialect)

	explicit := Ticket{ID: 100, Title: "explicit"}
	if err := store.Create(ctx, meta, &explicit); err != nil {
		t.Fatalf("Create with ID 100: %v", err)
	}
	auto := Ticket{Title: "auto"}
	if err := store.Create(ctx, meta, &auto); err != nil {
		t.Fatalf("Create without ID after ID 100: %v", err)
	}
	if auto.ID <= 100 {
		t.Fatalf("auto ID = %d, want > 100", auto.ID)
	}

	for _, want := range []Ticket{explicit, auto} {
		var got Ticket
		if err := store.Get(ctx, meta, want.ID, &got); err != nil {
			t.Fatalf("Get %d: %v", want.ID, err)
		}
		if got != want {
			t.Fatalf("Get %d = %+v, want %+v", want.ID, got, want)
		}
	}
}

func TestCreateWithLowerExplicitIDDoesNotMoveTheSequenceBack(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, Ticket{})
	meta, _ := registry.Get("Ticket")
	store := db.NewStore(sqlDB, dialect)

	for _, id := range []int64{100, 50} {
		if err := store.Create(ctx, meta, &Ticket{ID: id, Title: "explicit"}); err != nil {
			t.Fatalf("Create with ID %d: %v", id, err)
		}
	}
	auto := Ticket{Title: "auto"}
	if err := store.Create(ctx, meta, &auto); err != nil {
		t.Fatalf("Create without ID: %v", err)
	}
	if auto.ID <= 100 {
		t.Fatalf("auto ID = %d, want > 100", auto.ID)
	}
	count, err := store.Count(ctx, meta, db.Query{})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Fatalf("Count = %d, want 3", count)
	}
}

func TestCreateAfterExplicitIDWorksOnAReservedWordTable(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, User{})
	meta, _ := registry.Get("User")
	store := db.NewStore(sqlDB, dialect)

	if err := store.Create(ctx, meta, &User{ID: 100, Name: "Ada", Group: "a"}); err != nil {
		t.Fatalf("Create user with ID 100: %v", err)
	}
	auto := User{Name: "Bob", Group: "b"}
	if err := store.Create(ctx, meta, &auto); err != nil {
		t.Fatalf("Create user without ID: %v", err)
	}
	if auto.ID <= 100 {
		t.Fatalf("auto user ID = %d, want > 100", auto.ID)
	}
}

func TestConcurrentCreatesGetDistinctIDsAlongsideAnExplicitID(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, Ticket{})
	meta, _ := registry.Get("Ticket")
	store := db.NewStore(sqlDB, dialect)

	const workers = 20
	tickets := make([]Ticket, workers)
	tickets[0] = Ticket{ID: 1000, Title: "explicit"}
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := range tickets {
		wg.Add(1)
		go func(ticket *Ticket) {
			defer wg.Done()
			errs <- store.Create(ctx, meta, ticket)
		}(&tickets[i])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Create: %v", err)
		}
	}

	seen := map[int64]bool{}
	for _, ticket := range tickets {
		if ticket.ID == 0 || seen[ticket.ID] {
			t.Fatalf("IDs not distinct and set: %+v", tickets)
		}
		seen[ticket.ID] = true
	}
	after := Ticket{Title: "after"}
	if err := store.Create(ctx, meta, &after); err != nil {
		t.Fatalf("Create after concurrent creates: %v", err)
	}
	if after.ID <= 1000 {
		t.Fatalf("ID after explicit 1000 = %d, want > 1000", after.ID)
	}
}

func TestCreateWithATakenExplicitIDFailsAndLaterCreatesStillWork(t *testing.T) {
	ctx := context.Background()
	sqlDB, dialect, registry := openTables(t, Ticket{})
	meta, _ := registry.Get("Ticket")
	store := db.NewStore(sqlDB, dialect)

	if err := store.Create(ctx, meta, &Ticket{ID: 50, Title: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, meta, &Ticket{ID: 50, Title: "second"}); err == nil {
		t.Fatal("Create with a taken explicit ID succeeded")
	}
	var count int
	if err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM Ticket").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows after the failed Create = %d (%v), want 1", count, err)
	}
	auto := Ticket{Title: "auto"}
	if err := store.Create(ctx, meta, &auto); err != nil {
		t.Fatalf("Create after the failure: %v", err)
	}
	if auto.ID <= 50 {
		t.Fatalf("auto ID = %d, want > 50", auto.ID)
	}
}

func TestCreateWithAnExplicitIDHonoursACancelledContext(t *testing.T) {
	sqlDB, dialect, registry := openTables(t, Ticket{})
	meta, _ := registry.Get("Ticket")
	store := db.NewStore(sqlDB, dialect)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.Create(ctx, meta, &Ticket{ID: 9, Title: "never"}); err == nil {
		t.Fatal("Create on a cancelled context succeeded")
	}
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM Ticket").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rows = %d (%v), want 0", count, err)
	}
}

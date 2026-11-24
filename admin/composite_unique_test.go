package admin_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/model"
)

type seatBooking struct {
	ID   int64 `tango:"pk"`
	Room string
	Slot string
}

// TestCompositeUniqueViolationIsAFormMessageWithoutTheValues shows a repeated
// (Room, Slot) pair is refused with the form-level message: the database names
// no single field, and the submitted values never reach the page.
func TestCompositeUniqueViolationIsAFormMessageWithoutTheValues(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(seatBooking{}, model.Unique("Room", "Slot")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(seatBooking{}, admin.Options{SingularName: "booking", PluralName: "bookings"}); err != nil {
		t.Fatal(err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	mustInsertBooking(t, sqlDB, "Zephyr-room", "Quokka-slot")

	response := doRequest(t, handler, http.MethodPost, "/admin/seat_booking/new/", url.Values{
		"Room": {"Zephyr-room"},
		"Slot": {"Quokka-slot"},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %.300s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "Another booking already uses one of these values.") {
		t.Fatalf("body lacks the form-level message: %.300s", body)
	}
	for _, leak := range []string{"UNIQUE", "constraint", "uniq_seat_booking", "internal error"} {
		if strings.Contains(body, leak) {
			t.Fatalf("body leaks %q", leak)
		}
	}
}

func mustInsertBooking(t *testing.T, sqlDB *sql.DB, room, slot string) {
	t.Helper()
	if _, err := sqlDB.Exec(`INSERT INTO seat_booking (room, slot) VALUES ($1, $2)`, room, slot); err != nil {
		t.Fatal(err)
	}
}

package ledger

import (
	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
)

// New is the ledger app: both row types in the admin, and a JSON list of
// customers so there is something to call.
func New(store *db.Store) tango.App {
	return tango.NewApp("ledger", func(registry *tango.Registry) error {
		if err := registry.Models().Register(Customers{}); err != nil {
			return err
		}
		if err := registry.Models().Register(Invoices{}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Customers{}, admin.Options{
			ListDisplay: []string{"Name", "Email", "Tier", "Since"},
			Search:      []string{"Name", "Email"},
			Ordering:    []string{"Name"},
		}); err != nil {
			return err
		}
		if err := registry.Admin().Register(Invoices{}, admin.Options{
			ListDisplay: []string{"CustomerID", "AmountCents", "IssuedOn", "PaidAt"},
			Ordering:    []string{"-IssuedOn"},
		}); err != nil {
			return err
		}
		meta, _ := registry.Models().Get("Customers")
		return registry.Routes().Include("/customers/", tango.URLs{
			tango.Path("GET", "/", func(ctx *tango.Context) error {
				var rows []Customers
				if err := store.List(ctx.Context(), meta, db.Query{OrderBy: []string{"Name"}, Limit: 100}, &rows); err != nil {
					return err
				}
				return ctx.JSON(200, rows)
			}, tango.Name("list")),
		})
	})
}

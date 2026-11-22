// Package ledger holds the row types of a billing database that already
// exists. The tags describe that database exactly, so the baseline migration
// reproduces it and tango finds nothing to change.
package ledger

import (
	"time"

	"github.com/angvp/tango/model"
)

// Customers is the customers table, created long ago by another tool.
type Customers struct {
	ID        int        `tango:"pk,int4"`
	Name      string     `tango:"varchar=80,notnull"`
	Email     string     `tango:"varchar=255,notnull,unique=idx_customers_email"`
	Tier      string     `tango:"varchar=20,notnull,default=standard"`
	Since     model.Date `tango:"notnull"`
	CreatedAt time.Time  `tango:"naive,notnull"`
}

// Invoices is the invoices table. PaidAt is nullable: an unpaid invoice has
// none, and tango stores a zero time as NULL.
type Invoices struct {
	ID          int        `tango:"pk,int4"`
	CustomerID  int        `tango:"int4,fk=Customers,notnull,index=idx_invoices_customer_id"`
	AmountCents int        `tango:"int4,notnull,default=0"`
	IssuedOn    model.Date `tango:"notnull"`
	PaidAt      time.Time  `tango:"naive"`
}

# Guide: application architecture

`tango newproject`/`tango newapp` (see [project structure](project-structure.md)) always start you at the same small shape. This guide covers what comes after: how to evolve a growing tanGO app's structure without guessing, and when not to bother.

There is no single "correct" tanGO project layout. There are three recommended shapes, forming an escalation path — move to the next one only once the app actually earns it. A small app that stays small for its whole life is the normal, unremarkable outcome, not a project that "hasn't gotten around to" the next shape yet.

## Small app

Use this when the app is mostly CRUD or a small site — the shape `tango newapp` already gives you:

```text
models.go
views.go
urls.go
admin.go
migrations/
templates/
```

A model struct here does double duty: it's both the persistence record (`db.Store` reads and writes it directly) and the closest thing the app has to a domain type. Views call `db.Store` directly. There's no separate service layer, no `ports/`, nothing to wire — see [project structure](project-structure.md) for what a freshly scaffolded app already contains, and the [`api-with-admin`](../../examples/api-with-admin) example for a small app in full.

## Medium app

Use this once views are starting to hold application decisions — validation that spans more than one field, multi-step writes, logic that would otherwise get copy-pasted across two views:

```text
models.go
services/
repositories/ (or store/)
views.go
urls.go
admin.go
migrations/
templates/
```

`services/` holds that logic so views stay thin translators of HTTP into a call. `repositories/`/`store/` wraps `db.Store` once direct calls scattered across views get noisy. `models.go` still serves as both persistence metadata and domain data — a Medium app has no `ports/` and no separate domain package; that split is what marks the next step, not this one.

## Hexagonal app

Use this once the app has real business rules, multiple interfaces (HTML and JSON, or a CLI alongside HTTP), reusable domain logic, or long-term maintenance pressure that makes the Medium shape's direct `services/` → `repositories/` coupling start to hurt:

```text
domain/          # entities, value objects, pure rules
ports/           # interfaces the application layer depends on
services/        # use cases and orchestration
adapters/db/     # SQL or tango db.Store persistence adapters
adapters/http/   # tango views, binders, render helpers, JSON responses
adapters/admin/  # tango admin registration and admin-specific widgets/options
urls.go          # URL contribution wiring
migrations/
```

This is the one shape where persistence-facing structs and domain objects become separate types: persistence models move into `adapters/db/`, domain objects stay in `domain/` with no `db`/`model` tags and no awareness that a database exists at all. Admin registration moves into `adapters/admin/` too, for the same reason — it's a framework-facing adapter, not a privileged layer that gets to stay at app root once everything else has moved out.

## Design principles

These hold across all three shapes, not just Hexagonal:

- Views translate HTTP into use cases; they don't own business rules.
- Admin configuration is an adapter, not a privileged domain layer.
- `domain/` and `services/` never depend on `tango.Context`, Chi, templates, or admin packages — those are framework-facing concerns that belong in `adapters/`.
- Persistence sits behind a small, project-owned interface once direct `db.Store` calls scattered through the codebase get noisy — a `repositories/`/`store/` package at the Medium tier, a `ports/` interface implemented by an `adapters/db/` type at the Hexagonal tier.

## Snippets

These are illustrative Go, written to the same APIs used elsewhere in this repo — not a checked-in, CI-built example module (this milestone is documentation only; see [limitations](../limitations.md) for what's actually shipped).

A view calling a service:

```go
// adapters/http/views.go
func checkoutBook(svc *services.CheckoutService) tango.View {
	return func(ctx *tango.Context) error {
		var req checkoutRequest
		if err := ctx.Bind(&req); err != nil {
			return err
		}
		order, err := svc.Checkout(ctx.Context(), req.BookID, req.CustomerID)
		if err != nil {
			return err
		}
		return ctx.JSON(201, order)
	}
}
```

A service depending on a port, not a concrete adapter:

```go
// ports/books.go
type BookRepository interface {
	FindByID(ctx context.Context, id int64) (domain.Book, error)
}

// services/checkout.go
type CheckoutService struct {
	books ports.BookRepository
}

func (s *CheckoutService) Checkout(ctx context.Context, bookID, customerID int64) (domain.Order, error) {
	book, err := s.books.FindByID(ctx, bookID)
	if err != nil {
		return domain.Order{}, err
	}
	return domain.NewOrder(book, customerID), nil
}
```

A DB adapter implementing that port:

```go
// adapters/db/books.go
type bookRepository struct {
	store *db.Store
	meta  model.ModelMeta
}

func (r *bookRepository) FindByID(ctx context.Context, id int64) (domain.Book, error) {
	var record BookRecord
	if err := r.store.Get(ctx, r.meta, id, &record); err != nil {
		return domain.Book{}, err
	}
	return record.ToDomain(), nil
}
```

An admin file staying thin:

```go
// adapters/admin/books.go
func RegisterBooks(registry *tango.Registry) error {
	return registry.Admin().Register(db.BookRecord{}, admin.Options{
		ListDisplay: []string{"Title", "AuthorID"},
	})
}
```

## When to stop

tanGO supports the Hexagonal shape; it never requires it. Most tanGO apps should stay Small. Moving to Medium or Hexagonal without the growth signals above just adds indirection with nothing behind it — a `ports/` package with exactly one implementation and no second interface to decouple from is ceremony, not architecture.

## See also

- [Project structure](project-structure.md) — what `tango newproject`/`tango newapp` actually generate.
- [Reusable apps](reusable-apps.md) — packaging an app as its own importable Go package, a separate axis from the shape it's structured in internally.
- `docs/agents/project-shape.md` — the compact, agent-facing version of this guide's escalation rules.

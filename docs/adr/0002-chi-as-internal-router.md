# Use Chi as an Internal Router

## Context

tanGO needs reliable HTTP routing, middleware compatibility, path parameters, and a mature routing implementation. Chi is idiomatic, small, and close to the Go standard library.

Gin is powerful and popular, but it brings a larger framework surface and a stronger programming model. tanGO should own its public API instead of exposing another framework's conventions.

## Decision

tanGO may use Chi internally for runtime routing.

The public API should expose tanGO concepts such as:

- `Path`
- `Include`
- named routes
- namespaces
- reverse routing
- `View func(*Context) error`

Chi types should not appear in public tanGO APIs.

## Consequences

This keeps tanGO's routing API stable even if the internal router changes later.

It also means tanGO must build and maintain a small adapter layer from its route declarations to Chi routes.

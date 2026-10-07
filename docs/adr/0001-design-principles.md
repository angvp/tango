# Founding Design Principles

## Context

tanGO is inspired by Django's strong developer experience, especially its admin, URL architecture, apps, and integrated workflow. Go has different strengths: explicit code, simple interfaces, standard library primitives, and a culture of avoiding unnecessary magic.

The project should preserve those Go strengths.

## Decision

tanGO will use these founding principles:

- Integrate Go primitives before replacing them.
- Prefer explicit structure over convention magic.
- Prefer declarative APIs before escape hatches.
- Make the admin a normal app using public framework APIs.
- Make HTML optional and JSON/API-only applications first-class.
- Use `html/template`; do not create a custom template language.
- Keep Chi replaceable behind tanGO abstractions.
- Avoid scaffolding commands for models/controllers in the early project.

## Consequences

This makes tanGO less magical than Django, especially around filesystem discovery and generated code. That is intentional.

The framework should help users compose normal Go programs rather than teach them a parallel language.

# A foreign key's target model is validated after every app registers, not when the referencing model registers

`model.Registry.Register` validates everything else about a model immediately and synchronously — a missing or duplicate primary key fails right at the `Register` call. Foreign keys break that pattern deliberately: `Post{AuthorID int64 \`tango:"fk=Author"\`}` can only be checked against a model actually named `Author`, and that model might not be registered yet at the moment `posts`'s `Register` runs, depending on `InstalledApps` order. Validating immediately would make `InstalledApps` order a real constraint for any app declaring a foreign key — a new, surprising requirement that contradicts how every other cross-app dependency in tanGO works today (order only matters for what an app's `Register` callback actually *reads* at call time, per the [configuration](../guides/configuration.md) guide, never for static tag metadata).

Instead, foreign key targets are checked once, in a pass that runs after `RunRegistration()` finishes installing every app — the same place `tango.Check`'s route-compilation and app-check enforcement already runs. An `fk=` tag naming a model that never got registered by any app fails there, with an error naming the missing model, regardless of which app declared the tag or where it sits in `InstalledApps`.

## Consequences

- A foreign key referencing a genuinely nonexistent model is only caught at `Check`/registration-completion time, not at the moment the offending `Register` call runs — slightly harder to pinpoint from a stack trace, though the error message names the missing model directly.
- Apps declaring foreign keys never need to coordinate `InstalledApps` position with the apps they reference, unlike an app that reads `registry.Admin()` state another app must have already contributed.

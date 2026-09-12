# Guide: app checks

An `App` may optionally implement one extra method to contribute advisory checks, without changing the required `App` interface (`Name() string`, `Register(*Registry) error`) at all:

```go
type Checker interface {
	Checks() []AppCheck
}

type AppCheck struct {
	Description string
	Err         error // nil means the check passed
}
```

```go
func (a App) Checks() []tango.AppCheck {
	return []tango.AppCheck{
		{Description: "at least one post exists", Err: a.validateHasPosts()},
	}
}
```

After `RunRegistration` completes, `Registry.Checks()` aggregates every installed app's `Checks()` result (apps that don't implement `Checker` are skipped, not an error) in `InstalledApps` order:

```go
checks := registry.Checks()
for _, c := range checks {
	if c.Err != nil {
		fmt.Println("FAIL:", c.Description, c.Err)
	}
}
```

**Current status:** `Registry.Checks()` is implemented and tested, but `tango check` / `tango.Check(config)` do **not** yet call it automatically — `-check` today only validates registration and route compilation. If you want app checks enforced by `tango check`, call `registry.Checks()` yourself in your `main.go`'s `-check` handling, after `RunRegistration`, and fail on any non-nil `Err`. Automatic aggregation into `tango check`'s pass/fail output is expected in a future milestone; this guide will be updated when it ships.

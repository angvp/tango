# Guide: app checks

An `App` may optionally implement one extra method to contribute checks enforced by `tango check`, without changing the required `App` interface (`Name() string`, `Register(*Registry) error`) at all:

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

`tango.Check(config)` and the generated `-check` flag both enforce app checks automatically. If any check returns a non-nil `Err`, `tango.Check` returns one aggregated error listing every failing check's `Description`; passing checks and apps that don't implement `Checker` do not affect the result. This means `tango check` validates registration, route compilation, and installed-app checks in one pass.

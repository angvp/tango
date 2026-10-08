# Reusable greetings: the reusable app

A tanGO app meant to be installed by other projects. The `greetings` package is the app: its model, admin registration, JSON routes, an embedded static asset and an app check. `migrations/` holds the migrations it contributes to every host that installs it.

`main.go` is only a development harness, so `tango check` and `tango makemigrations` have something to run against. It is not how the app is run: see [`reusable-greetings-host`](../reusable-greetings-host) for a host project installing it.

## Work on it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/reusable-greetings
go run . -check
go test ./...
```

Generate the app's contributed migrations after changing its model with `tango makemigrations` from this directory.

See the [reusable apps guide](../../docs/guides/reusable-apps.md).

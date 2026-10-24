# Contributing to tanGO

Thanks for considering a contribution. tanGO is small on purpose, so the most useful changes are usually a focused fix, a clearer doc or a test for something that was untested. For anything larger than that, open an issue first so we can agree on the shape before you write it.

## Set up

You need the Go version named in [`go.mod`](go.mod) and Git. A database server is optional: the tests use SQLite unless you point them elsewhere.

```sh
git clone https://github.com/angvp/tango
cd tango
git checkout develop
scripts/check.sh
```

`scripts/check.sh` runs what CI runs: formatting, `go vet`, build, the tests, a guard against committed binaries and the offline Markdown link check. If it passes on your machine, CI will pass.

To run the tests against PostgreSQL or MySQL, set `TANGO_TEST_DSN` to a `postgres://` or `mysql://` URL for a throwaway database (the `testdb` package reads it; the dialect-sensitive packages are listed in `scripts/dialect-manifest.txt`). The Redis and Memcached adapter tests skip unless `TANGO_TEST_REDIS_URL` and `TANGO_TEST_MEMCACHE_ADDR` are set.

## Where changes go

- Branch from `develop` and open your pull request against `develop`. `main` only receives releases.
- Keep history linear: rebase on `develop` rather than merging it into your branch.
- One logical change per pull request.

## What a change needs

- **Tests.** Behaviour changes come with a test that fails without the change. Tests assert behaviour, not wording in documentation.
- **Documentation.** If you change a public API, a CLI command or a documented behaviour, update the affected guide, [`docs/reference.md`](docs/reference.md) and, when it exists, the matching recipe in [`docs/agents/`](docs/agents/README.md).
- **Changelog.** Add a line under `[Unreleased]` in [`CHANGELOG.md`](CHANGELOG.md) for anything a user can notice.
- **Source only.** Do not commit built executables, database files, coverage profiles or other machine-local artifacts. `scripts/check.sh` fails when you do.
- **Plain description.** Docs, comments and changelog entries describe tanGO on its own terms and do not compare it to other frameworks.

## Commits

- Sign off every commit with `git commit -s`.
- Write the subject in the imperative (`fix: reject empty migration names`).
- Do not put issue or pull request numbers in commit messages; link them in the pull request instead.

## Reporting problems

- Bugs and feature requests: [open an issue](https://github.com/angvp/tango/issues/new/choose).
- Security problems: follow [SECURITY.md](SECURITY.md); do not open a public issue.
- Questions about how to use tanGO: start with the [tutorial](docs/tutorial/01-bootstrap-routing-json.md) and the [guides](docs/guides/).

By contributing you agree that your work is released under the project's [MIT license](LICENSE).

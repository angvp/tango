# Releasing tanGO

This is the maintainer's checklist for cutting a release. What a release promises is in [Versioning and compatibility](docs/compatibility.md); this page is how one is made. The **Release** workflow (`.github/workflows/release.yml`) runs on a pushed tag. It checks the tag, creates the GitHub release, and moves the website to it.

## Where work lands

Work for the next minor release goes to the **`develop`** branch, not `main`. `main` only ever holds released code and the commit being released, so the website's `main` links (and anyone cloning) never see unreleased behaviour. CI runs on every push, so every `develop` commit is tested on both dialects.

To release, make `main` equal to a green `develop` commit with a fast-forward, so the SHA CI already passed on is the one you tag:

```sh
git checkout main && git merge --ff-only develop && git push origin main
```

A fix that cannot wait for the next minor goes to `main` first, is merged back into `develop`, and is released as a patch.

## Before tagging

1. **Pick the version.**
   - A **patch** (`v0.1.1`) may not break the Covered API, and the workflow refuses one that `gorelease` finds incompatible.
   - A **minor** (`v0.2.0`) may break the Covered API, but only for things whose Deprecation window has passed.
   - Changes to tanGO's own `admin`/`accounts` models ship only in a minor release.
2. **Finish `CHANGELOG.md`** on `develop`:
   - rename `## [Unreleased]` to `## [X.Y.Z] - YYYY-MM-DD`, dated the day you tag;
   - add a fresh, empty `## [Unreleased]` above it;
   - update the comparison links at the bottom.

   The workflow uses the section as the release notes, and refuses a tag without a dated section. Every breaking entry needs an upgrade note, and so does every model change with its "run `tango makemigrations`, then apply" instruction.
3. **Add the release's migration files to the Generated-file contract tests**. From the repository root:

   ```sh
   internal/migrationcompat/generate.sh vX.Y.Z local
   ```

   Then add the new `vX_Y_Z` directory to `Generators` in `internal/migrationcompat/versions.go` (see [the fixtures' README](internal/migrationcompat/README.md)). Run them on both dialects:

   ```sh
   go test ./internal/migrationcompat/ ./internal/cli/
   TANGO_TEST_DSN="postgres://…" go test ./internal/migrationcompat/ ./internal/cli/
   ```
4. **Commit and push to `develop`, wait for CI to pass, then fast-forward `main` to that commit (see above) and confirm CI passed on it.** The workflow doesn't rerun the tests. It refuses a tag unless `lint`, `test (sqlite)` and `test (postgres)` all succeeded on the commit being tagged.
5. **Optionally, check before you tag.** For a minor release, run:

   ```sh
   GH_TOKEN=$(gh auth token) go run ./internal/releasecheck/cmd/releasecheck -tag vX.Y.Z -commit "$(git rev-parse origin/main)"
   ```

   For a patch release, also pass `-gorelease` a report from `go run golang.org/x/exp/cmd/gorelease@latest -base=<previous tag>`. Before the tag exists, the check never asks the Go module proxy about it: the proxy would cache "unknown revision" for up to about half an hour and delay the release's website update.

## Tag

```sh
git tag -a vX.Y.Z -m vX.Y.Z <commit>
git push origin vX.Y.Z
```

**Never move or delete a pushed tag once the release exists.** The Go module proxy and checksum database keep the first commit they see for a version forever, so moving the tag changes nothing for users and only makes the repository disagree with what they download. The workflow refuses a tag the proxy already serves from another commit. If a release turns out wrong, release the next patch version.

If the workflow refuses the tag, it only asks the proxy once every other check passes. So until then you may fix the problem, delete the tag, and push it again:
- if CI was still running, re-run the workflow once CI finishes;
- if the commit was wrong, fix it on `main`, then move the tag to the fixed commit.

## After tagging

1. Watch the **Release** workflow. On a minor release, its job summary shows `gorelease`'s report: check it against the changelog's upgrade notes.
2. Check that the [GitHub release](https://github.com/angvp/tango/releases) exists, with the changelog section as its notes.
3. Check the website. The workflow dispatches **Follow a tanGO release** in `angvp/tango-web`, which pins the release in its `go.mod`, tests the site against it, and pushes to its `main` for Railway to deploy. Within a few minutes [tangoframework.com](https://tangoframework.com) should show "Docs for vX.Y.Z". If the dispatch step failed (for example, an expired token), run that workflow by hand from `tango-web`'s Actions tab with the version. The workflow must be on `tango-web`'s default branch for either to work.
4. Check that [pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango) lists the version. Requesting the page fetches it if it doesn't.

## The website dispatch token

`TANGO_WEB_DISPATCH_TOKEN`, a secret in this repository's Actions settings, lets the Release workflow start the website's workflow. It is a fine-grained personal access token with these settings:

- **Resource owner:** `angvp`.
- **Expiration:** one year.
- **Repository access:** only `angvp/tango-web`.
- **Permissions:** **Actions: Read and write**, which is all a workflow dispatch needs. Metadata: Read-only is added automatically. Nothing else.

Create it at GitHub → Settings → Developer settings → Fine-grained tokens, then store it with `gh secret set TANGO_WEB_DISPATCH_TOKEN --repo angvp/tango`.

Don't record the token's expiry date anywhere public. GitHub emails the token's owner before it expires: renew it then, and update the secret the same way. If a release's website step fails with an authentication error, the token has expired or been revoked.

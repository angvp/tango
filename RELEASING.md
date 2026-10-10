# Releasing tanGO

This is the maintainer's checklist for cutting a release. What a release promises is in [Versioning and compatibility](docs/compatibility.md); this page is how one is made. The **Release** workflow (`.github/workflows/release.yml`) runs on a pushed tag: it checks the tag, creates the GitHub release, and moves the website to it. The sections after the short path say why each rule exists.

## The short path

For version `vX.Y.Z`, with `develop` holding the work and green CI on its latest commit:

1. **Pick the version** (see [Why the version](#why-the-version-and-its-upgrade-notes)). Then make the mechanical edits:

   ```sh
   scripts/prepare-release.sh vX.Y.Z            # --date YYYY-MM-DD to override today's UTC date
   ```

   It dates the changelog section, adds a fresh `[Unreleased]`, updates the comparison links, and generates and registers the version's migration-compat corpus. It only edits files: it never commits, tags or pushes, and running it again changes nothing.
2. **Review the diff** and finish what a script cannot: every breaking entry in the changelog needs an upgrade note, and so does every model change, with its "run `tango makemigrations`, then apply" instruction. Commit to `develop` and push.
3. **Wait for CI on that commit**, then rehearse:

   ```sh
   scripts/release-dryrun.sh vX.Y.Z
   ```

   It runs `scripts/check.sh`, compares the API with the previous tag, and runs the release check in dry-run mode. It changes nothing and refuses a dirty tree. A pass means everything the real check looks at is in order **except** two things it deliberately skips and names: that the commit is on `main`, and that the module proxy doesn't already serve the tag from another commit.
4. **Fast-forward `main` to that commit**, so the SHA CI already passed on is the one you tag:

   ```sh
   git checkout main && git merge --ff-only develop && git push origin main
   ```

   Confirm CI passed on `main`'s commit.
5. **Tag** (never move or delete a pushed tag, see [Why tags are permanent](#why-tags-are-permanent)):

   ```sh
   git tag -a vX.Y.Z -m vX.Y.Z <commit>
   git push origin vX.Y.Z
   ```
6. **Read the Release job**: the outcomes are in [After the tag](#after-the-tag).
7. **Check pkg.go.dev** lists the version ([pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango); requesting the page fetches it if it doesn't).

Then rebase `develop` onto `main` if they differ:

```sh
git checkout develop && git rebase main && git push --force-with-lease origin develop
```

## After the tag

The Release job refuses the tag, creating nothing, unless the tagged commit is on `main`, CI passed on that exact commit, the changelog has a dated section for the version, the version's migration-compat corpus is committed and listed, a patch release has no incompatible change, and the module proxy doesn't already serve the tag from another commit. It then creates the GitHub release and moves the website. Its outcomes:

- **Green:** the release exists with the changelog section as its notes, and within a few minutes [tangoframework.com](https://tangoframework.com) shows "Docs for vX.Y.Z". On a minor release, the job summary shows `gorelease`'s report: check it against the changelog's upgrade notes.
- **Red before "Create the GitHub release":** the tag was refused and nothing was created. Read the listed reasons. The proxy is asked last, only once every other check passes, so until then you may fix the problem, delete the tag, and push it again: re-run the workflow once CI finishes if CI was still running, or fix the commit on `main` and move the tag to it.
- **Red at "Move the website to the release":** **the release is published and the website is not updated.** Nothing is undone. Look at the failed run in `angvp/tango-web`'s Actions tab, fix the cause, and run **Follow a tanGO release** there by hand with the version. If it never started, the dispatch token may have lapsed (see [the token](#the-website-dispatch-token)). The workflow must be on `tango-web`'s default branch for either to work.

## Adapter modules

`storage/s3` is its own Go module (see [ADR 0054](docs/adr/0054-cloud-storage-adapters-are-separate-go-modules.md)). The root tag `vX.Y.Z` does not version it: it is tagged `storage/s3/vA.B.C` on the commit it ships from, after the root tag it requires exists, and the module's `go.mod` requires that root version (the local `replace` is for development only and is removed in the commit that is tagged). Run `scripts/check.sh`, which also tests the module, before tagging either.

## Why

### Where work lands

Work for the next minor release goes to the **`develop`** branch, not `main`. `main` only ever holds released code and the commit being released, so the website's `main` links (and anyone cloning) never see unreleased behaviour. CI runs on every push, so every `develop` commit is tested on both dialects.

History stays linear: no merge commits, ever. A fix that cannot wait for the next minor goes to `main` first and is released as a patch; then rebase `develop` onto it as above. Rewriting `develop` this way is expected, since it is unreleased and shared by no one else. `main` is only ever fast-forwarded (`--ff-only`), and a rebase is how any branch catches up with it.

### Why the version and its upgrade notes

- A **patch** (`v0.1.1`) may not break the Covered API, and the workflow refuses one that `gorelease` finds incompatible.
- A **minor** (`v0.2.0`) may break the Covered API, but only for things whose Deprecation window has passed.
- Changes to tanGO's own `admin`/`accounts` models ship only in a minor release.

The workflow uses the changelog section as the release notes and refuses a tag without a dated section, which is why the date is the day you tag.

### Why docs-only fixes are patch releases

The website renders the docs of the pinned release tag, never `main`, so it claims only released behaviour. A docs fix on `main` is therefore invisible until a release, and a docs-only fix is released as a patch. That is the price of the rule, and the short path makes it cheap.

### Why the migration-compat corpus

The [Generated-file contract](docs/compatibility.md#the-generated-file-contract) promises each release's `tango makemigrations` output keeps working on every later v0.x release. The tests that hold it to that read `internal/migrationcompat`, and forgetting to add a release's files there is silent, so the tag check refuses a version whose directory is missing or not listed in `Generators` (see [the fixtures' README](internal/migrationcompat/README.md)). `prepare-release.sh` generates it; never edit a released version's files by hand.

To run those tests on both dialects:

```sh
go test ./internal/migrationcompat/ ./internal/cli/
TANGO_TEST_DSN="postgres://…" go test ./internal/migrationcompat/ ./internal/cli/
```

### Why tags are permanent

**Never move or delete a pushed tag once the release exists.** The Go module proxy and checksum database keep the first commit they see for a version forever, so moving the tag changes nothing for users and only makes the repository disagree with what they download. The workflow refuses a tag the proxy already serves from another commit. If a release turns out wrong, release the next patch version.

For the same reason a dry run never asks the proxy about a tag that isn't pushed: it would cache "unknown revision" for up to about half an hour and delay the real release's website update.

### The website dispatch token

`TANGO_WEB_DISPATCH_TOKEN`, a secret in this repository's Actions settings, lets the Release workflow start the website's workflow and read its run. It is a fine-grained personal access token with these settings:

- **Resource owner:** `angvp`.
- **Expiration:** one year.
- **Repository access:** only `angvp/tango-web`.
- **Permissions:** **Actions: Read and write**, which is all a workflow dispatch needs. Metadata: Read-only is added automatically. Nothing else.

Create it at GitHub → Settings → Developer settings → Fine-grained tokens, then store it with `gh secret set TANGO_WEB_DISPATCH_TOKEN --repo angvp/tango`.

The weekly **Release health** workflow (`scripts/release-health.sh`) checks that the token can still read `tango-web`'s workflows and that the live site shows the latest release (allowing an hour after a release), so a lapsed token or a stuck deploy shows up as a failed run before a release needs it. It never prints the token or its expiry.

Don't record the token's expiry date anywhere public. GitHub emails the token's owner before it expires: renew it then, and update the secret the same way. If a release's website step fails with an authentication error, or Release health goes red about the token, it has expired or been revoked.

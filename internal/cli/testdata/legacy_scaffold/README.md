# The scaffold from before `tango shell`

`admin/` and `noadmin/` hold the `main.go`, `migrations/migrations.go` and
`.gitignore` that `tango newproject legacyapp` (with and without `--no-admin`)
wrote at commit `3e6cb3a`, the last one before the shell, with the `.txt`
suffix so Go ignores them. **Never regenerate them**: they stand for projects
that already exist.

`upgrade_admin.diff` and `upgrade_noadmin.diff` are the documented upgrade for
those two scaffolds: they move the app list and configuration into
`project/project.go`, call it from `main.go`, and add `shell/main.go`. When
`tango newproject` changes what it writes, regenerate these two (a `git diff`
between the fixture and the new scaffold, with the path prefixes trimmed to
`a/` and `b/`); `TestUpgradingAPreShellProjectFollowsTheDocumentedDiff` fails
until they match.

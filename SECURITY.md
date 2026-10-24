# Security policy

## Reporting a vulnerability

Please report a suspected vulnerability privately, not in a public issue or pull request.

Use GitHub's private reporting: open the repository's **Security** tab and choose **Report a vulnerability**. Include the tanGO version, what you expected and what happened, and the smallest example that reproduces it.

You can expect an acknowledgement within a week. If the report is confirmed, the fix ships in a release whose notes say it is a security fix, and credit is given if you want it.

## Supported versions

tanGO is pre-1.0. Security fixes go into the latest released minor version; older versions are not patched. [Versioning and compatibility](docs/compatibility.md) explains that a security fix may break the Covered API in any release, and says so in its release notes.

## What is in scope

A flaw in tanGO's own code: the admin and `accounts` authentication, CSRF protection, the rate limiter, session handling, request binding, SQL generation, the `storage` and `mail` packages, and the `tango` CLI.

[Limitations](docs/limitations.md#security-boundaries) lists the documented security boundaries, such as the in-memory, per-process login rate limiter. Behaviour described there is a known limit, not a vulnerability, but a way to get around a boundary the documentation promises is.

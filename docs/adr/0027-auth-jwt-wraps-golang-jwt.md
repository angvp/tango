# `auth/jwt` wraps `golang-jwt/jwt/v5`

tanGO delegates JWT parsing and signature verification to `github.com/golang-jwt/jwt/v5` rather than implementing security-sensitive token parsing itself.

The dependency remains behind `github.com/angvp/tango/auth/jwt`: public claims, keys, options, and sentinel errors are tanGO-owned, and no third-party type appears in the public API. That boundary keeps the contract explicit and permits replacing the implementation without changing host applications.

JWT support lives in a subpackage so projects using only database-backed sessions do not mix JWT-specific key and lifetime configuration into the base `auth` package.

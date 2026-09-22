# JWT API example

This example shows a JSON route protected by tanGO's optional `auth/jwt`
package. Its hardcoded key and `-issue-token` flag are development-only tools,
not a production credential or authentication endpoint.

```sh
go run . -issue-token reader-42
go run .
curl -H 'Authorization: Bearer <token>' http://localhost:8000/api/me
```

A real application loads its keys from secure configuration and issues tokens
only after its own credential check. There is deliberately no unauthenticated
HTTP token endpoint in this example.

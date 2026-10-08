# JWT API example

This example shows a JSON route protected by tanGO's optional `auth/jwt`
package. Its hardcoded key and `-issue-token` flag are development-only tools,
not a production credential or authentication endpoint.

```sh
git clone https://github.com/angvp/tango
cd tango/examples/jwt-api
go run . -issue-token reader-42
go run .
curl -H 'Authorization: Bearer <token>' http://localhost:8000/api/me
```

The address is `TANGO_ADDR`, else the `PORT` hosting platforms set, else `:8000`. Ctrl-C or `SIGTERM` shuts the server down gracefully.

A real application loads its keys from secure configuration and issues tokens
only after its own credential check. There is deliberately no unauthenticated
HTTP token endpoint in this example.

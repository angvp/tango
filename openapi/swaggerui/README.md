# Vendored Swagger UI

`assets/` holds three files from the `swagger-ui-dist` npm package, release
**5.17.14** (Apache-2.0; `LICENSE` and `NOTICE` are kept beside them):

| file | sha256 |
|---|---|
| `swagger-ui-bundle.js` | `c2e4a9ef08144839ff47c14202063ecfe4e59e70a4e7154a26bd50d880c88ba1` |
| `swagger-ui.css` | `40170f0ee859d17f92131ba707329a88a070e4f66874d11365e9a77d232f6117` |

Only the bundle and the stylesheet are used; the package's `index.html`,
standalone preset and source maps are left out.

## Updating

1. In an empty directory outside the repository: `npm pack swagger-ui-dist@<version>` (npm checks the tarball's integrity), then extract it.
2. Copy `swagger-ui-bundle.js`, `swagger-ui.css`, `LICENSE` and `NOTICE` into `assets/`.
3. Update `Version` in `swaggerui.go` and the table above (`shasum -a 256`).
4. Run `go test ./openapi/...` and open `/docs/` in a browser against an app with a documented route.
5. Record the version in the changelog.

## Under a Content-Security-Policy

`WithContentSecurityPolicy` serves the page under the application's policy with the three changes Swagger UI needs: `style-src` allows `'unsafe-inline'`, `img-src` allows `data:`, and `connect-src` allows `'self'`. The changes apply to the UI page's route only. See the [content security policy guide](../../docs/guides/content-security-policy.md#swagger-ui).

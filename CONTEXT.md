# Public terminology

This glossary names concepts that are easy to confuse in tanGO's public API.

## JWT claims

`auth/jwt.Claims{Subject, Issuer, Audience, IssuedAt, ExpiresAt}` is the fixed decoded payload returned by `auth/jwt.Service`. `Subject` is an opaque host-owned identity, not an `accounts.Account` or another model. Claims are stateless and have no database row behind them.

Do not call decoded claims an Application session: sessions are persisted, revocable rows managed by `auth` primitives.

## JWT signing key

`auth/jwt.Key{ID, Secret}` is one HMAC key identified by `kid`. A service has one active key for issuing new tokens and a fixed set of retired verification-only keys. The active key is automatically included in the verification set. Rotation is manual across deployments; v0.1 has no JWKS, remote loading, or automatic rotation.

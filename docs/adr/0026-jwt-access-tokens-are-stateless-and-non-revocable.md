# JWT access tokens are stateless and non-revocable

`auth/jwt.Service.Verify` performs cryptographic and registered-claim validation without a database lookup. This keeps access-token verification stateless for JSON API and future WebSocket use cases.

The consequence is explicit: an issued token cannot be invalidated before `exp`. There is no blacklist, revocation store, account-status lookup, or refresh-token mechanism in v0.1. Expiry is the only containment mechanism.

Applications that need immediate revocation should use tanGO's database-backed cookie sessions or build a host-owned revocation layer. Adding a framework revocation mechanism later requires a separate storage, cleanup, and verification-latency design.

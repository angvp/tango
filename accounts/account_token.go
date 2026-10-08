package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/angvp/tango/db"
)

// Token lifetimes and the per-address cooldown between emails.
const (
	resetTokenLifetime        = time.Hour
	verificationTokenLifetime = 24 * time.Hour
	mailCooldown              = 5 * time.Minute
)

// hashSecret is the SHA-256, in hex, that AccountToken stores for a token
// or a normalized address.
func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// issueToken deletes expired tokens and account's earlier tokens of
// purpose, then stores and returns a new one, valid for lifetime from now.
func issueToken(ctx context.Context, store *db.Store, account Account, purpose TokenPurpose, now time.Time, lifetime time.Duration) (string, error) {
	_, _, tokenMeta := accountModelMetas()
	var stale []AccountToken
	query := db.Query{Where: []db.Condition{{Field: "ExpiresAt", Op: db.OpLte, Value: now}}}
	if err := store.List(ctx, tokenMeta, query, &stale); err != nil {
		return "", err
	}
	var earlier []AccountToken
	query = db.Query{Where: []db.Condition{{Field: "AccountID", Op: db.OpEq, Value: account.ID}, {Field: "Purpose", Op: db.OpEq, Value: string(purpose)}}}
	if err := store.List(ctx, tokenMeta, query, &earlier); err != nil {
		return "", err
	}
	for _, row := range append(stale, earlier...) {
		if err := store.Delete(ctx, tokenMeta, row.ID); err != nil && !errors.Is(err, db.ErrNotFound) {
			return "", err
		}
	}

	token, err := randomToken()
	if err != nil {
		return "", err
	}
	row := AccountToken{
		TokenHash:   hashSecret(token),
		AccountID:   account.ID,
		Purpose:     purpose,
		AddressHash: hashSecret(normalizeEmail(account.Email)),
		ExpiresAt:   now.Add(lifetime),
	}
	if err := store.Create(ctx, tokenMeta, &row); err != nil {
		return "", err
	}
	return token, nil
}

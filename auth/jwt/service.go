// Package jwt provides explicit, stateless HS256 access-token primitives.
package jwt

import (
	"errors"
	"fmt"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const (
	MinimumSecretBytes = 32
	DefaultMaxTTL      = 24 * time.Hour
	DefaultClockSkew   = time.Duration(0)
	maximumClockSkew   = 5 * time.Minute
)

var (
	ErrInvalidToken = errors.New("tango jwt: invalid token")
	ErrExpiredToken = errors.New("tango jwt: expired token")
	ErrMissingToken = errors.New("tango jwt: missing token")
)

// Claims is tanGO's fixed, registered-claims-only JWT payload.
type Claims struct {
	Subject   string
	Issuer    string
	Audience  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Key is one host-owned HS256 signing key identified by kid.
type Key struct {
	ID     string
	Secret []byte
}

type serviceConfig struct {
	maxTTL time.Duration
	skew   time.Duration
}

// ServiceOption configures a Service at construction.
type ServiceOption func(*serviceConfig)

// WithClockSkew permits the configured amount of clock drift during time
// validation. Values above five minutes are rejected by NewService.
func WithClockSkew(skew time.Duration) ServiceOption {
	return func(config *serviceConfig) { config.skew = skew }
}

// WithMaxTTL changes the maximum lifetime Issue accepts.
func WithMaxTTL(ttl time.Duration) ServiceOption {
	return func(config *serviceConfig) { config.maxTTL = ttl }
}

// Service issues and verifies tokens for one issuer and audience. Its key set
// is immutable after construction.
type Service struct {
	active   Key
	keys     map[string][]byte
	issuer   string
	audience string
	maxTTL   time.Duration
	skew     time.Duration
	now      func() time.Time
}

// NewService constructs an HS256 service. active signs new tokens and is
// automatically included alongside verificationKeys during verification.
func NewService(active Key, verificationKeys []Key, issuer string, audience string, opts ...ServiceOption) (*Service, error) {
	config := serviceConfig{maxTTL: DefaultMaxTTL, skew: DefaultClockSkew}
	for _, option := range opts {
		if option != nil {
			option(&config)
		}
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, fmt.Errorf("tango jwt: issuer must not be empty")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("tango jwt: audience must not be empty")
	}
	if config.maxTTL <= 0 {
		return nil, fmt.Errorf("tango jwt: maximum TTL must be positive")
	}
	if config.skew < 0 || config.skew > maximumClockSkew {
		return nil, fmt.Errorf("tango jwt: clock skew must be between 0 and %s", maximumClockSkew)
	}

	keys := make(map[string][]byte, len(verificationKeys)+1)
	allKeys := append([]Key{active}, verificationKeys...)
	for _, key := range allKeys {
		if strings.TrimSpace(key.ID) == "" {
			return nil, fmt.Errorf("tango jwt: key ID must not be empty")
		}
		if len(key.Secret) < MinimumSecretBytes {
			return nil, fmt.Errorf("tango jwt: key %q secret must be at least %d bytes", key.ID, MinimumSecretBytes)
		}
		if _, exists := keys[key.ID]; exists {
			return nil, fmt.Errorf("tango jwt: duplicate key ID %q", key.ID)
		}
		keys[key.ID] = append([]byte(nil), key.Secret...)
	}
	active.Secret = append([]byte(nil), active.Secret...)
	return &Service{
		active: active, keys: keys, issuer: issuer, audience: audience,
		maxTTL: config.maxTTL, skew: config.skew, now: time.Now,
	}, nil
}

// Issue signs a token for subject with the active key.
func (s *Service) Issue(subject string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(subject) == "" {
		return "", fmt.Errorf("tango jwt: subject must not be empty")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("tango jwt: TTL must be positive")
	}
	if ttl > s.maxTTL {
		return "", fmt.Errorf("tango jwt: TTL %s exceeds maximum %s", ttl, s.maxTTL)
	}
	now := s.now().UTC()
	claims := jwtlib.RegisteredClaims{
		Subject: subject, Issuer: s.issuer, Audience: jwtlib.ClaimStrings{s.audience},
		IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(now.Add(ttl)),
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	token.Header["kid"] = s.active.ID
	encoded, err := token.SignedString(s.active.Secret)
	if err != nil {
		return "", fmt.Errorf("tango jwt: sign token: %w", err)
	}
	return encoded, nil
}

// Verify validates token's algorithm, key, signature, issuer, audience and
// timestamps. It never reads a database.
func (s *Service) Verify(encoded string) (Claims, error) {
	registered := jwtlib.RegisteredClaims{}
	parser := jwtlib.NewParser(jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}), jwtlib.WithoutClaimsValidation())
	token, err := parser.ParseWithClaims(encoded, &registered, func(token *jwtlib.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("missing kid")
		}
		secret, ok := s.keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown kid")
		}
		return secret, nil
	})
	if err != nil || token == nil || !token.Valid {
		return Claims{}, fmt.Errorf("%w: verification failed", ErrInvalidToken)
	}
	if registered.Subject == "" || registered.Issuer != s.issuer || !containsAudience(registered.Audience, s.audience) || registered.IssuedAt == nil || registered.ExpiresAt == nil {
		return Claims{}, fmt.Errorf("%w: required claims do not match", ErrInvalidToken)
	}
	now := s.now().UTC()
	if !registered.IssuedAt.Time.Before(registered.ExpiresAt.Time) {
		return Claims{}, fmt.Errorf("%w: expiry must follow issued-at", ErrInvalidToken)
	}
	if !now.Before(registered.ExpiresAt.Time.Add(s.skew)) {
		return Claims{}, fmt.Errorf("%w: token expired", ErrExpiredToken)
	}
	if now.Add(s.skew).Before(registered.IssuedAt.Time) {
		return Claims{}, fmt.Errorf("%w: issued-at is in the future", ErrInvalidToken)
	}
	return Claims{
		Subject: registered.Subject, Issuer: registered.Issuer, Audience: s.audience,
		IssuedAt: registered.IssuedAt.Time, ExpiresAt: registered.ExpiresAt.Time,
	}, nil
}

func containsAudience(audiences jwtlib.ClaimStrings, audience string) bool {
	for _, candidate := range audiences {
		if candidate == audience {
			return true
		}
	}
	return false
}

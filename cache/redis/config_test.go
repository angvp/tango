package redis

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"
)

func TestOptionsRejectsBadConfigs(t *testing.T) {
	for name, tt := range map[string]struct {
		cfg  Config
		want string
	}{
		"no URL":                {Config{}, "URL is required"},
		"a plain host":          {Config{URL: "localhost:6379"}, "scheme"},
		"an http URL":           {Config{URL: "http://localhost:6379"}, "scheme"},
		"a unix socket URL":     {Config{URL: "unix:///tmp/redis.sock"}, "scheme"},
		"TLS config with redis": {Config{URL: "redis://localhost:6379", TLSConfig: &tls.Config{}}, "rediss://"},
		"a negative dial":       {Config{URL: "redis://localhost", DialTimeout: -1}, "negative"},
		"a negative read":       {Config{URL: "redis://localhost", ReadTimeout: -1}, "negative"},
		"a negative write":      {Config{URL: "redis://localhost", WriteTimeout: -1}, "negative"},
		"a negative pool":       {Config{URL: "redis://localhost", PoolSize: -1}, "negative"},
		"a bad database":        {Config{URL: "redis://localhost:6379/notanumber"}, "invalid URL"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := options(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("options = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestOptionsTakesTheDatabaseFromTheURLPath(t *testing.T) {
	opts, err := options(Config{URL: "redis://localhost:6379/3"})
	if err != nil || opts.DB != 3 || opts.Addr != "localhost:6379" {
		t.Fatalf("options = %+v, %v; want DB 3 on localhost:6379", opts, err)
	}
	opts, _ = options(Config{URL: "redis://localhost:6379"})
	if opts.DB != 0 {
		t.Fatalf("DB = %d without a path, want 0", opts.DB)
	}
}

func TestExplicitCredentialsOverrideTheURLUserinfo(t *testing.T) {
	opts, err := options(Config{URL: "redis://urluser:urlpass@localhost:6379"})
	if err != nil || opts.Username != "urluser" || opts.Password != "urlpass" {
		t.Fatalf("URL userinfo: %+v, %v", opts, err)
	}
	opts, _ = options(Config{URL: "redis://urluser:urlpass@localhost:6379", Username: "bob", Password: "secret"})
	if opts.Username != "bob" || opts.Password != "secret" {
		t.Fatalf("explicit credentials did not override: %q/%q", opts.Username, opts.Password)
	}
	opts, _ = options(Config{URL: "redis://urluser:urlpass@localhost:6379", Password: "only-pass"})
	if opts.Username != "urluser" || opts.Password != "only-pass" {
		t.Fatalf("only the non-empty field should override: %q/%q", opts.Username, opts.Password)
	}
}

func TestRedissAcceptsATLSConfigAndTimeoutsApply(t *testing.T) {
	custom := &tls.Config{ServerName: "cache.internal", MinVersion: tls.VersionTLS13}
	opts, err := options(Config{
		URL: "rediss://localhost:6380", TLSConfig: custom,
		DialTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 3 * time.Second, PoolSize: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TLSConfig != custom {
		t.Fatal("the supplied TLSConfig was not used")
	}
	if opts.DialTimeout != time.Second || opts.ReadTimeout != 2*time.Second || opts.WriteTimeout != 3*time.Second || opts.PoolSize != 7 {
		t.Fatalf("timeouts or pool not applied: %+v", opts)
	}
	plain, _ := options(Config{URL: "rediss://localhost:6380"})
	if plain.TLSConfig == nil {
		t.Fatal("rediss:// without a TLSConfig must still use TLS")
	}
	if p, _ := options(Config{URL: "redis://localhost"}); p.TLSConfig != nil {
		t.Fatal("redis:// must not use TLS")
	}
}

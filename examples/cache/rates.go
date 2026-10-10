package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/cache"
)

// rateTTL is how long a computed rate is served from the cache. A cache only
// ever makes a read cheaper; after this the next request recomputes.
const rateTTL = 30 * time.Second

// Rate is the recomputable read this example caches: an exchange rate that is
// expensive to work out. The cache holds its JSON.
type Rate struct {
	Currency   string    `json:"currency"`
	Rate       float64   `json:"rate"`
	ComputedAt time.Time `json:"computed_at"`
}

// calculator is the expensive source of truth. Loads counts how often it ran,
// so the example and its tests can see what the cache saved.
type calculator struct {
	now   func() time.Time
	loads atomic.Int64
}

func (c *calculator) compute(currency string) (Rate, error) {
	c.loads.Add(1)
	rates := map[string]float64{"EUR": 0.92, "GBP": 0.79, "JPY": 149.5}
	rate, ok := rates[currency]
	if !ok {
		return Rate{}, errUnknownCurrency
	}
	return Rate{Currency: currency, Rate: rate, ComputedAt: c.now().UTC()}, nil
}

var errUnknownCurrency = errors.New("unknown currency")

// ratesApp serves GET /rates/{currency}/ through FetchJSON. The cache is
// passed in, visible at the call site: nothing about it is global.
func ratesApp(store cache.Store, calc *calculator) tango.App {
	return tango.NewApp("rates", func(registry *tango.Registry) error {
		return registry.Routes().Include("/rates/", tango.URLs{
			tango.Path("GET", "/{currency}/", rateView(store, calc), tango.Name("detail")),
		})
	})
}

func rateView(store cache.Store, calc *calculator) tango.View {
	return func(ctx *tango.Context) error {
		currency := strings.ToUpper(ctx.Param("currency"))
		loaded := false
		rate, err := cache.FetchJSON(ctx.Context(), store, "rate:"+currency, rateTTL,
			func(_ context.Context) (Rate, error) {
				loaded = true
				return calc.compute(currency)
			},
			// A cache failure never fails the request: FetchJSON falls back to
			// computing the rate. The hook is where the host records it.
			cache.OnError(func(op string, err error) {
				ctx.Logger().Warn("cache failure", "op", op, "err", err)
			}),
		)
		if errors.Is(err, errUnknownCurrency) {
			return ctx.JSON(http.StatusNotFound, map[string]string{"error": "unknown currency"})
		}
		if err != nil {
			return err
		}
		source := "hit"
		if loaded {
			source = "miss"
		}
		ctx.ResponseWriter().Header().Set("X-Cache", source)
		return ctx.JSON(http.StatusOK, rate)
	}
}

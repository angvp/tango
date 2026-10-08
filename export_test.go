package tango

import (
	"log/slog"

	"github.com/angvp/tango/observability"
)

// ObserveRoutes gives registry's route tree a logger and a metrics
// recorder, as ServeContext does, so tests outside the package can observe
// a handler without starting a server.
func ObserveRoutes(registry *Registry, logger *slog.Logger, recorder observability.Recorder) {
	registry.Routes().setObservability(logger, recorder, recorder != nil)
}

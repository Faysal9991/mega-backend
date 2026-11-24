package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/config"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/go-chi/chi/v5"
)

// NewRouter composes infrastructure once. Future modules register explicitly on
// the /api/v1 subrouter; an empty registration list creates no business routes.
func NewRouter(cfg config.HTTPConfig, logger *slog.Logger, lifecycle *Lifecycle, register ...func(chi.Router)) (http.Handler, error) {
	if logger == nil || lifecycle == nil || cfg.MaxConcurrentRequests < 1 {
		return nil, errors.New("httpserver: validated limits, logger and lifecycle are required")
	}
	responses, err := response.New(logger)
	if err != nil {
		return nil, err
	}
	router := chi.NewRouter()
	notFound := func(w http.ResponseWriter, r *http.Request) { infrastructureError(responses, w, r, apperror.NotFound) }
	router.NotFound(notFound)
	// Custom 405 bodies must explicitly preserve Allow; route probes use fresh
	// contexts so completion logging keeps the original matched route context.
	methodNotAllowed := func(w http.ResponseWriter, r *http.Request) {
		// A side-effect-free route probe collects the allowed registered methods.
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		allowed := make([]string, 0, 9)
		for _, method := range []string{http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace} {
			probe := chi.NewRouteContext()
			if router.Match(probe, method, path) {
				allowed = append(allowed, method)
			}
		}
		if len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		} else {
			infrastructureError(responses, w, r, apperror.NotFound)
			return
		}
		infrastructureError(responses, w, r, apperror.MethodNotAllowed)
	}
	router.MethodNotAllowed(methodNotAllowed)
	mountHealth(router, lifecycle, responses)
	if len(register) > 0 {
		router.Route("/api/v1", func(api chi.Router) {
			for _, mount := range register {
				if mount != nil {
					mount(api)
				}
			}
		})
	}
	// Chi skips its middleware stack on an entirely empty mux. A dedicated
	// infrastructure mount runs the chain even before business routes exist.
	root := chi.NewRouter()
	root.Use(requestID, requestLog(logger), recoverPanic(logger, responses), admission(cfg.MaxConcurrentRequests, responses), bodyLimit(cfg.MaxBodyBytes, responses), requestDeadline(cfg.RequestTimeout, responses))
	root.NotFound(notFound)
	root.MethodNotAllowed(methodNotAllowed)
	root.Mount("/", router)
	return root, nil
}

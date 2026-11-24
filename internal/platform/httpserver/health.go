package httpserver

import (
	"net/http"
	"sync/atomic"

	"github.com/Faysal9991/mega-backend/internal/platform/apperror"
	"github.com/Faysal9991/mega-backend/internal/platform/response"
	"github.com/go-chi/chi/v5"
)

const (
	lifecycleStarting uint32 = iota
	lifecycleReady
	lifecycleStopped
)

// Lifecycle is shared by one router and its single-use Server. Its zero value
// is unready. Do not copy it after use. Only the Server changes readiness.
type Lifecycle struct{ phase atomic.Uint32 }

func (l *Lifecycle) Ready() bool { return l.phase.Load() == lifecycleReady }
func (l *Lifecycle) markReady()  { l.phase.CompareAndSwap(lifecycleStarting, lifecycleReady) }

// Terminal unready prevents startup/shutdown races from restoring readiness.
func (l *Lifecycle) markUnready() { l.phase.Store(lifecycleStopped) }

// Use the same escaped-path preference as Chi; encoded aliases are not probes.
func isHealthPath(r *http.Request) bool {
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	return path == "/health/live" || path == "/health/ready"
}

func mountHealth(router chi.Router, lifecycle *Lifecycle, responses *response.Writer) {
	live := func(w http.ResponseWriter, r *http.Request) {
		if err := responses.Success(w, r, http.StatusOK, "HEALTH_LIVE", struct{}{}, response.Options{}); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	ready := func(w http.ResponseWriter, r *http.Request) {
		if !lifecycle.Ready() {
			w.Header().Set("Retry-After", "1")
			infrastructureError(responses, w, r, apperror.ServiceUnavailable)
			return
		}
		if err := responses.Success(w, r, http.StatusOK, "HEALTH_READY", struct{}{}, response.Options{}); err != nil {
			panic(http.ErrAbortHandler)
		}
	}
	router.Get("/health/live", live)
	router.Head("/health/live", live)
	router.Get("/health/ready", ready)
	router.Head("/health/ready", ready)
}

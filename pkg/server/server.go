package server

import (
	"net/http"

	"github.com/jonasohland/consul-register/pkg/register"
	htx "github.com/jonasohland/ext/http"
)

type handler struct {
	svc register.RegistrationService
	mux http.ServeMux
}

// ServeHTTP implements http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Health(); err != nil {
		htx.J(w, err)
	} else {
		htx.J(w, htx.NewMessageBody("healthy"))
	}
}

func (h *handler) reload(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Reload(r.Context()); err != nil {
		htx.J(w, err)
	} else {
		htx.J(w, htx.NewMessageBody("ok"))
	}
}

func NewHandler(svc register.RegistrationService) http.Handler {
	handler := &handler{
		svc: svc,
	}
	handler.mux.HandleFunc("GET /v1/health", handler.health)
	handler.mux.HandleFunc("POST /v1/reload", handler.reload)
	return handler
}

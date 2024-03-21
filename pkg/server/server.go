package server

import (
	"net/http"

	"github.com/jonasohland/consul-registrator/pkg/registrator"
	htx "github.com/jonasohland/ext/http"
)

type handler struct {
	rgt registrator.Registrator
	mux http.ServeMux
}

// ServeHTTP implements http.Handler.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.rgt.Health(); err != nil {
		htx.J(w, err)
	} else {
		htx.J(w, htx.NewMessageBody("healthy"))
	}
}

func (h *handler) reload(w http.ResponseWriter, r *http.Request) {
	htx.J(w, htx.NewMessageBody("ok"))
}

func NewHandler(registrator registrator.Registrator) http.Handler {
	handler := &handler{
		rgt: registrator,
	}
	handler.mux.HandleFunc("GET /health", handler.health)
	handler.mux.HandleFunc("POST /reload", handler.reload)
	return handler
}

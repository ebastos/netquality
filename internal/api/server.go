package api

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/eval"
	"github.com/ebastos/netquality/internal/store"
)

//go:embed web/*
var webFS embed.FS

// Server wires the JSON API handlers and static web UI to the underlying store and engine.
type Server struct {
	cfg    *config.Config
	db     *store.DB
	engine *eval.Engine
}

// New constructs the HTTP server mux (API routes + static files under /).
func New(cfg *config.Config, db *store.DB, engine *eval.Engine) *Server {
	return &Server{cfg: cfg, db: db, engine: engine}
}

// Handler returns the http.Handler (ready to be served by http.Server).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	h := &handlers{srv: s}

	mux.HandleFunc("GET /api/v1/status", h.status)
	mux.HandleFunc("GET /api/v1/incidents", h.listIncidents)
	mux.HandleFunc("GET /api/v1/incidents/{id}", h.getIncident)
	mux.HandleFunc("GET /api/v1/incidents/{id}/export", h.exportIncident)
	mux.HandleFunc("GET /api/v1/samples", h.samples)
	mux.HandleFunc("GET /api/v1/rollups", h.rollups)

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(webRoot))
	mux.Handle("/", fileServer)

	return mux
}

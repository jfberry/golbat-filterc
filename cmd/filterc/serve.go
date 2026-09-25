package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jfberry/golbat-filterc/filterc"
	"github.com/jfberry/golbat-filterc/golbat"
)

const maxBodyBytes = 64 << 10

type compileRequest struct {
	Expression string `json:"expression"`
	Bounds     *struct {
		Min filterc.LatLon `json:"min"`
		Max filterc.LatLon `json:"max"`
	} `json:"bounds"`
	Limit *int `json:"limit"`
}

type position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type errorResponse struct {
	Error struct {
		Message  string    `json:"message"`
		Position *position `json:"position,omitempty"`
	} `json:"error"`
}

type compileResponse struct {
	Request  filterc.ScanRequest  `json:"request"`
	Warnings []string             `json:"warnings"`
	Response *golbat.ScanResponse `json:"response,omitempty"`
}

type server struct {
	s      settings
	client *golbat.Client // nil when no Golbat URL is configured
}

// newHandler serves POST /compile, POST /scan and GET /healthz. There is
// no authentication: the server is a local tool, and its /scan carries the
// configured secret's scan capability.
func newHandler(s settings, client *golbat.Client) http.Handler {
	sv := &server{s: s, client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("POST /compile", sv.compile)
	mux.HandleFunc("POST /scan", sv.scan)
	return mux
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, pos *position) {
	var body errorResponse
	body.Error.Message, body.Error.Position = msg, pos
	writeJSONStatus(w, status, body)
}

// build reads and compiles the request; on failure it has written the
// error response and returns ok=false.
func (sv *server) build(w http.ResponseWriter, r *http.Request) (out compileResponse, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var in compileRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body over 64 KiB", nil)
			return out, false
		}
		writeError(w, http.StatusBadRequest, "bad JSON: "+err.Error(), nil)
		return out, false
	}
	compiled, err := filterc.Compile(in.Expression)
	if err != nil {
		var ce *filterc.Error
		if errors.As(err, &ce) {
			writeError(w, http.StatusBadRequest, ce.Msg, &position{Line: ce.Pos.Line, Column: ce.Pos.Column})
		} else {
			writeError(w, http.StatusBadRequest, err.Error(), nil)
		}
		return out, false
	}
	bounds, limit := sv.s.bounds(), sv.s.Bounds.Limit
	if in.Bounds != nil {
		bounds = filterc.Bounds{Min: in.Bounds.Min, Max: in.Bounds.Max}
	}
	if in.Limit != nil {
		limit = *in.Limit
	}
	out.Request = compiled.Request(bounds, limit)
	out.Warnings = compiled.Warnings
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out, true
}

func (sv *server) compile(w http.ResponseWriter, r *http.Request) {
	out, ok := sv.build(w, r)
	if !ok {
		return
	}
	writeJSONStatus(w, http.StatusOK, out)
}

func (sv *server) scan(w http.ResponseWriter, r *http.Request) {
	if sv.client == nil {
		writeError(w, http.StatusServiceUnavailable, "no golbat url configured (--golbat or [golbat] url in filterc.toml)", nil)
		return
	}
	out, ok := sv.build(w, r)
	if !ok {
		return
	}
	resp, err := sv.client.ScanPokemon(r.Context(), out.Request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), nil)
		return
	}
	out.Response = resp
	writeJSONStatus(w, http.StatusOK, out)
}

func runServe(s settings, stdout, stderr io.Writer) int {
	var client *golbat.Client
	if s.Golbat.URL != "" {
		client = &golbat.Client{URL: s.Golbat.URL, Secret: s.Golbat.Secret, HTTP: &http.Client{Timeout: 60 * time.Second}}
	}
	srv := &http.Server{Addr: s.Listen, Handler: newHandler(s, client), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	fmt.Fprintf(stdout, "filterc listening on %s (golbat: %q)\n", s.Listen, s.Golbat.URL)
	select {
	case err := <-errc:
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		return 0
	}
}

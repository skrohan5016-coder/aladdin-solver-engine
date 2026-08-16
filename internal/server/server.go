package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

type Server struct {
	cfg      solve.Config
	log      *slog.Logger
	rec      *record.Recorder
	maxBytes int64
}

func New(cfg solve.Config, log *slog.Logger, rec *record.Recorder) *Server {
	return &Server{cfg: cfg, log: log, rec: rec, maxBytes: 64 << 20}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /solve", s.handleSolve)
	mux.HandleFunc("POST /notify", s.handleNotify)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func (s *Server) handleSolve(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body := http.MaxBytesReader(w, r.Body, s.maxBytes)
	defer body.Close()

	var auction api.Auction
	dec := json.NewDecoder(body)
	if err := dec.Decode(&auction); err != nil {
		s.log.Warn("bad auction payload", "err", err)
		http.Error(w, "invalid auction", http.StatusBadRequest)
		return
	}

	// Respect the auction deadline; the driver cancels anything later.
	ctx := r.Context()
	if dl, err := time.Parse(time.RFC3339, auction.Deadline); err == nil {
		budget := time.Until(dl) - 200*time.Millisecond
		if budget <= 0 {
			s.writeJSON(w, api.SolveResponse{Solutions: []api.Solution{}})
			return
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}

	res := solve.Solve(ctx, &auction, s.cfg)
	if res.Solutions == nil {
		res.Solutions = []api.Solution{}
	}

	id := ""
	if auction.ID != nil {
		id = *auction.ID
	}
	elapsed := time.Since(start)
	s.log.Info("solved",
		"auction", id,
		"orders", res.Stats.Orders,
		"pools", res.Stats.PoolsUsable,
		"cows", res.Stats.CoWMatches,
		"routes", res.Stats.BaselineRoutes,
		"solutions", res.Stats.Solutions,
		"ms", elapsed.Milliseconds(),
	)
	if s.rec != nil {
		s.rec.Auction(id, &auction, res, elapsed)
	}

	s.writeJSON(w, api.SolveResponse{Solutions: res.Solutions})
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	defer body.Close()

	var n api.Notification
	if err := json.NewDecoder(body).Decode(&n); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	// Notifications are the only feedback channel that says why a bid lost.
	// They are the most valuable thing this service collects.
	s.log.Info("notify", "auction", n.AuctionID, "solution", n.SolutionID, "kind", n.Kind)
	if s.rec != nil {
		s.rec.Notification(n)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("encode response", "err", err)
	}
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, map[string]string{"status": "ok"})
	})
	return mux
}

func (s *Server) handleSolve(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	body := http.MaxBytesReader(w, r.Body, s.maxBytes)
	defer body.Close()

	var auction api.Auction
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&auction); err != nil {
		s.log.Warn("bad auction payload", "err", err)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "auction payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid auction", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		s.log.Warn("auction payload has trailing data", "err", err)
		http.Error(w, "invalid auction", http.StatusBadRequest)
		return
	}

	deadline, err := time.Parse(time.RFC3339, auction.Deadline)
	if err != nil {
		s.log.Warn("invalid auction deadline", "deadline", auction.Deadline, "err", err)
		http.Error(w, "invalid auction deadline", http.StatusBadRequest)
		return
	}
	budget := time.Until(deadline) - 200*time.Millisecond
	if budget <= 0 {
		s.writeJSON(w, api.SolveResponse{Solutions: []api.Solution{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), budget)
	defer cancel()

	result := solve.Solve(ctx, &auction, s.cfg)
	if result.Solutions == nil {
		result.Solutions = []api.Solution{}
	}

	id := ""
	if auction.ID != nil {
		id = *auction.ID
	}
	elapsed := time.Since(start)
	s.log.Info("solved",
		"auction", id,
		"orders", result.Stats.Orders,
		"pools", result.Stats.PoolsUsable,
		"cows", result.Stats.CoWMatches,
		"routes", result.Stats.BaselineRoutes,
		"solutions", result.Stats.Solutions,
		"ms", elapsed.Milliseconds(),
	)
	if s.rec != nil {
		s.rec.Auction(id, &auction, result, elapsed)
	}

	s.writeJSON(w, api.SolveResponse{Solutions: result.Solutions})
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	defer body.Close()

	var notification api.Notification
	if err := json.NewDecoder(body).Decode(&notification); err != nil {
		// The notify endpoint is deliberately best-effort: a telemetry parsing
		// problem must not make the driver retry or affect settlement handling.
		w.WriteHeader(http.StatusOK)
		return
	}
	s.log.Info("notify",
		"auction", notification.AuctionID,
		"solution", notification.SolutionID,
		"kind", notification.Kind,
	)
	if s.rec != nil {
		s.rec.Notification(notification)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) writeJSON(w http.ResponseWriter, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		s.log.Error("encode response", "err", err)
		http.Error(w, "encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		s.log.Error("write response", "err", err)
	}
}

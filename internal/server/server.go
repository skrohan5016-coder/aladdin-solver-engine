package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
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
	if err := decodeUniqueJSON(body, &auction); err != nil {
		s.log.Warn("bad auction payload", "err", err)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "auction payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid auction", http.StatusBadRequest)
		return
	}
	if err := validateAuction(&auction); err != nil {
		s.log.Warn("invalid auction contract", "err", err)
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
		if err := s.rec.Auction(id, &auction, result, elapsed); err != nil {
			s.log.Error("record auction", "auction", id, "err", err)
		}
	}

	s.writeJSON(w, api.SolveResponse{Solutions: result.Solutions})
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	defer body.Close()

	var notification api.Notification
	if err := decodeUniqueJSON(body, &notification); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	s.log.Info("notify",
		"auction", notification.AuctionID,
		"solution", notification.SolutionID,
		"kind", notification.Kind,
	)
	if s.rec != nil {
		if err := s.rec.Notification(notification); err != nil {
			s.log.Error("record notification", "auction", notification.AuctionID, "err", err)
		}
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

func validateAuction(auction *api.Auction) error {
	if auction.Tokens == nil || auction.Orders == nil || auction.Liquidity == nil ||
		auction.SurplusCapturingJitOrderOwner == nil {
		return errors.New("missing required auction collection")
	}
	if !validU256(auction.EffectiveGasPrice, true) {
		return errors.New("invalid effective gas price")
	}
	for address, token := range auction.Tokens {
		if address == "" || !validU256(token.AvailableBalance, true) {
			return fmt.Errorf("invalid token %q", address)
		}
		if token.ReferencePrice != "" && !validU256(token.ReferencePrice, true) {
			return fmt.Errorf("invalid reference price for token %q", address)
		}
	}
	for i, order := range auction.Orders {
		if order.UID == "" || order.SellToken == "" || order.BuyToken == "" ||
			!validU256(order.SellAmount, false) || !validU256(order.BuyAmount, false) ||
			!validU256(order.FullBuyAmount, false) {
			return fmt.Errorf("invalid order %d", i)
		}
		if order.FullSellAmount != "" && !validU256(order.FullSellAmount, false) {
			return fmt.Errorf("invalid full sell amount for order %d", i)
		}
	}
	for i, liquidity := range auction.Liquidity {
		if !decimalDigits(liquidity.ID, 20) {
			return fmt.Errorf("invalid liquidity id at index %d", i)
		}
		if _, err := strconv.ParseUint(liquidity.ID, 10, 64); err != nil {
			return fmt.Errorf("invalid liquidity id at index %d: %w", i, err)
		}
	}
	return nil
}

func validU256(raw string, allowZero bool) bool {
	if !decimalDigits(raw, 78) {
		return false
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() < 0 || value.BitLen() > 256 {
		return false
	}
	return allowZero || value.Sign() > 0
}

func decimalDigits(raw string, max int) bool {
	if raw == "" || len(raw) > max {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return false
		}
	}
	return true
}

func decodeUniqueJSON(reader io.Reader, target any) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if err := validateUniqueObjectKeys(data); err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func validateUniqueObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple top-level JSON values")
		}
		return err
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	return nil
}

// Command solver runs the CoW Protocol solver engine.
//
// It listens for auctions from a CoW driver and returns proposed solutions.
// It holds no keys, opens no RPC connection, signs nothing, and submits
// nothing on-chain. All liquidity comes from the auction payload itself.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/server"
	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(env("LOG_LEVEL", "info")),
	}))

	cfg := solve.DefaultConfig()
	cfg.SettlementOverheadGas = envUint("SETTLEMENT_OVERHEAD_GAS", cfg.SettlementOverheadGas)
	cfg.PerTradeGas = envUint("PER_TRADE_GAS", cfg.PerTradeGas)
	cfg.RequireProfitable = envBool("REQUIRE_PROFITABLE", cfg.RequireProfitable)
	cfg.MaxSolutions = int(envUint("MAX_SOLUTIONS", uint64(cfg.MaxSolutions)))
	cfg.MaxOrders = int(envUint("MAX_ORDERS", uint64(cfg.MaxOrders)))

	rec, err := record.New(env("RECORD_DIR", "./data"), envBool("RECORD_FULL_AUCTIONS", false))
	if err != nil {
		log.Error("recorder init failed, continuing without it", "err", err)
		rec = nil
	} else {
		defer rec.Close()
	}

	srv := &http.Server{
		Addr:              env("LISTEN_ADDR", ":8000"),
		Handler:           server.New(cfg, log, rec).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("solver engine listening",
			"addr", srv.Addr,
			"requireProfitable", cfg.RequireProfitable,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envUint(k string, def uint64) uint64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}

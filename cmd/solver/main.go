// Command solver runs the CoW Protocol shadow solver engine.
//
// It listens for auctions from a local CoW driver and returns proposed
// solutions. It holds no keys, opens no RPC connection, signs nothing, and
// submits nothing on-chain. All liquidity comes from the auction payload.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	cfg.MaxSolutions = envPositiveInt("MAX_SOLUTIONS", cfg.MaxSolutions)
	cfg.MaxOrders = envPositiveInt("MAX_ORDERS", cfg.MaxOrders)
	cfg.MaxPools = envPositiveInt("MAX_POOLS", cfg.MaxPools)

	recorder, err := record.New(env("RECORD_DIR", "./data"), envBool("RECORD_FULL_AUCTIONS", false))
	if err != nil {
		log.Error("recorder init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := recorder.Close(); err != nil {
			log.Error("recorder close", "err", err)
		}
	}()

	listenAddr := env("LISTEN_ADDR", "127.0.0.1:8000")
	if err := validateListenAddr(listenAddr); err != nil {
		log.Error("unsafe listen address", "addr", listenAddr, "err", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           server.New(cfg, log, recorder).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Info("solver engine listening",
			"addr", httpServer.Addr,
			"requireProfitable", cfg.RequireProfitable,
			"maxOrders", cfg.MaxOrders,
			"maxPools", cfg.MaxPools,
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envUint(key string, fallback uint64) uint64 {
	if value := os.Getenv(key); value != "" {
		if number, err := strconv.ParseUint(value, 10, 64); err == nil {
			return number
		}
	}
	return fallback
}

func envPositiveInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	number, err := strconv.ParseUint(value, 10, 64)
	maxInt := uint64(^uint(0) >> 1)
	if err != nil || number == 0 || number > maxInt {
		return fallback
	}
	return int(number)
}

func validateListenAddr(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("host %q is not loopback", host)
	}
	return nil
}

func envBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func parseLevel(value string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return slog.LevelInfo
	}
	return level
}

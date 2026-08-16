#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def write_text(path: str, content: str) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content.lstrip("\n"), encoding="utf-8")


def canonical_bytes(value: object) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n").encode()


def write_json(path: str, value: object) -> None:
    target = ROOT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(canonical_bytes(value))


api_types = r'''
// Package api contains the wire types for the CoW Protocol solver-engine
// interface, as defined by the pinned cowprotocol/services contract.
//
// All token amounts are base-10 uint256 strings. They are never parsed into
// floats anywhere in this codebase.
package api

import (
	"bytes"
	"encoding/json"
)

// ---------- Auction (driver -> solver) ----------

type Auction struct {
	ID                            *string              `json:"id"`
	Tokens                        map[string]TokenInfo `json:"tokens"`
	Orders                        []Order              `json:"orders"`
	Liquidity                     []Liquidity          `json:"liquidity"`
	EffectiveGasPrice             string               `json:"effectiveGasPrice"`
	Deadline                      string               `json:"deadline"`
	SurplusCapturingJitOrderOwner []string             `json:"surplusCapturingJitOrderOwners"`
}

type TokenInfo struct {
	Decimals         *int   `json:"decimals,omitempty"`
	Symbol           string `json:"symbol,omitempty"`
	ReferencePrice   string `json:"referencePrice,omitempty"`
	AvailableBalance string `json:"availableBalance"`
	Trusted          bool   `json:"trusted"`
}

type Order struct {
	UID               string            `json:"uid"`
	SellToken         string            `json:"sellToken"`
	BuyToken          string            `json:"buyToken"`
	SellAmount        string            `json:"sellAmount"`
	FullSellAmount    string            `json:"fullSellAmount"`
	BuyAmount         string            `json:"buyAmount"`
	FullBuyAmount     string            `json:"fullBuyAmount"`
	FeePolicies       []FeePolicy       `json:"feePolicies,omitempty"`
	ValidTo           int64             `json:"validTo"`
	Kind              string            `json:"kind"`
	Receiver          string            `json:"receiver,omitempty"`
	Owner             string            `json:"owner"`
	PartiallyFillable bool              `json:"partiallyFillable"`
	PreInteractions   []json.RawMessage `json:"preInteractions"`
	PostInteractions  []json.RawMessage `json:"postInteractions"`
	SellTokenSource   string            `json:"sellTokenSource"`
	BuyTokenDest      string            `json:"buyTokenDestination"`
	Class             string            `json:"class"`
	AppData           string            `json:"appData"`
	FlashloanHint     json.RawMessage   `json:"flashloanHint,omitempty"`
	Wrappers          []json.RawMessage `json:"wrappers,omitempty"`
	SigningScheme     string            `json:"signingScheme"`
	Signature         string            `json:"signature"`
}

type FeePolicy struct {
	Kind            string   `json:"kind"`
	Factor          *float64 `json:"factor,omitempty"`
	MaxVolumeFactor *float64 `json:"maxVolumeFactor,omitempty"`
	Quote           *Quote   `json:"quote,omitempty"`
}

type Quote struct {
	SellAmount string `json:"sellAmount"`
	BuyAmount  string `json:"buyAmount"`
	Fee        string `json:"fee"`
}

// Liquidity is the flattened pinned union. Kinds that are not implemented by
// the router are still represented so fixtures can be losslessly normalized
// and so newly introduced semantic fields cannot be silently discarded.
type Liquidity struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Address     string `json:"address"`
	GasEstimate string `json:"gasEstimate"`

	Tokens json.RawMessage `json:"tokens,omitempty"`
	Fee    string          `json:"fee,omitempty"`
	Router string          `json:"router,omitempty"`

	BalancerPoolID string `json:"balancerPoolId,omitempty"`
	Version        string `json:"version,omitempty"`

	AmplificationParameter string `json:"amplificationParameter,omitempty"`

	SqrtPrice    string            `json:"sqrtPrice,omitempty"`
	Liquidity    string            `json:"liquidity,omitempty"`
	Tick         *int32            `json:"tick,omitempty"`
	LiquidityNet map[string]string `json:"liquidityNet,omitempty"`

	Hash                string `json:"hash,omitempty"`
	MakerToken          string `json:"makerToken,omitempty"`
	TakerToken          string `json:"takerToken,omitempty"`
	MakerAmount         string `json:"makerAmount,omitempty"`
	TakerAmount         string `json:"takerAmount,omitempty"`
	TakerTokenFeeAmount string `json:"takerTokenFeeAmount,omitempty"`
}

type TokenReserve struct {
	Balance string `json:"balance"`
}

// ---------- Solution (solver -> driver) ----------

type SolveResponse struct {
	Solutions []Solution `json:"solutions"`
}

type Solution struct {
	ID           uint64            `json:"id"`
	Prices       map[string]string `json:"prices"`
	Trades       []Trade           `json:"trades"`
	Interactions []Interaction     `json:"interactions"`
	Gas          uint64            `json:"gas,omitempty"`
}

type Trade struct {
	Kind           string `json:"kind"`
	Order          string `json:"order"`
	ExecutedAmount string `json:"executedAmount"`
	Fee            string `json:"fee,omitempty"`
}

// Interaction follows the runtime solvers-dto contract. Its ID is the same
// opaque string received in the auction, and internalize is an explicit bool.
type Interaction struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	InputToken   string `json:"inputToken"`
	OutputToken  string `json:"outputToken"`
	InputAmount  string `json:"inputAmount"`
	OutputAmount string `json:"outputAmount"`
	Internalize  bool   `json:"internalize"`
}

// ---------- Notification (driver -> solver) ----------

// Notification keeps all unknown metadata because notification payloads are
// explicitly extensible. json.Number preserves IDs above float64 precision.
type Notification struct {
	AuctionID  string                     `json:"auctionId"`
	SolutionID json.Number                `json:"solutionId"`
	Kind       string                     `json:"kind"`
	Extra      map[string]json.RawMessage `json:"-"`
}

func (n *Notification) UnmarshalJSON(data []byte) error {
	*n = Notification{}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["auctionId"]; ok {
		if err := json.Unmarshal(raw, &n.AuctionID); err != nil {
			return err
		}
		delete(fields, "auctionId")
	}
	if raw, ok := fields["solutionId"]; ok {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&n.SolutionID); err != nil {
			return err
		}
		delete(fields, "solutionId")
	}
	if raw, ok := fields["kind"]; ok {
		if err := json.Unmarshal(raw, &n.Kind); err != nil {
			return err
		}
		delete(fields, "kind")
	}
	n.Extra = fields
	return nil
}

func (n Notification) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(n.Extra)+3)
	for key, value := range n.Extra {
		fields[key] = value
	}
	put := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields[key] = raw
		return nil
	}
	if err := put("auctionId", n.AuctionID); err != nil {
		return nil, err
	}
	solutionID := n.SolutionID
	if solutionID == "" {
		solutionID = json.Number("0")
	}
	if err := put("solutionId", solutionID); err != nil {
		return nil, err
	}
	if err := put("kind", n.Kind); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}
'''
write_text("internal/api/types.go", api_types)

wire_contract = r'''
package server

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type objectRule struct {
	allowed  map[string]struct{}
	required map[string]struct{}
}

func names(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func rule(allowed, required []string) objectRule {
	return objectRule{allowed: names(allowed...), required: names(required...)}
}

func validatePinnedAuctionJSON(data []byte) error {
	top, err := validateObject(data, "auction", rule(
		[]string{"id", "tokens", "orders", "liquidity", "effectiveGasPrice", "deadline", "surplusCapturingJitOrderOwners"},
		[]string{"tokens", "orders", "liquidity", "effectiveGasPrice", "deadline", "surplusCapturingJitOrderOwners"},
	))
	if err != nil {
		return err
	}

	var tokens map[string]json.RawMessage
	if err := json.Unmarshal(top["tokens"], &tokens); err != nil {
		return fmt.Errorf("tokens: %w", err)
	}
	for address, raw := range tokens {
		if _, err := validateObject(raw, "token "+address, rule(
			[]string{"decimals", "symbol", "referencePrice", "availableBalance", "trusted"},
			[]string{"availableBalance", "trusted"},
		)); err != nil {
			return err
		}
	}

	var orders []json.RawMessage
	if err := json.Unmarshal(top["orders"], &orders); err != nil {
		return fmt.Errorf("orders: %w", err)
	}
	for index, raw := range orders {
		fields, err := validateObject(raw, fmt.Sprintf("order %d", index), rule(
			[]string{
				"uid", "sellToken", "buyToken", "sellAmount", "fullSellAmount", "buyAmount", "fullBuyAmount",
				"feePolicies", "validTo", "kind", "receiver", "owner", "partiallyFillable", "preInteractions",
				"postInteractions", "sellTokenSource", "buyTokenDestination", "class", "appData", "flashloanHint",
				"wrappers", "signingScheme", "signature",
			},
			[]string{
				"uid", "sellToken", "buyToken", "sellAmount", "fullSellAmount", "buyAmount", "fullBuyAmount",
				"validTo", "kind", "owner", "partiallyFillable", "preInteractions", "postInteractions",
				"sellTokenSource", "buyTokenDestination", "class", "appData", "signingScheme", "signature",
			},
		))
		if err != nil {
			return err
		}
		if err := validateInteractionDataArray(fields["preInteractions"], fmt.Sprintf("order %d preInteractions", index)); err != nil {
			return err
		}
		if err := validateInteractionDataArray(fields["postInteractions"], fmt.Sprintf("order %d postInteractions", index)); err != nil {
			return err
		}
		if raw, ok := fields["feePolicies"]; ok && !isJSONNull(raw) {
			if err := validateFeePolicies(raw, fmt.Sprintf("order %d feePolicies", index)); err != nil {
				return err
			}
		}
		if raw, ok := fields["flashloanHint"]; ok && !isJSONNull(raw) {
			if _, err := validateObject(raw, fmt.Sprintf("order %d flashloanHint", index), rule(
				[]string{"liquidityProvider", "protocolAdapter", "receiver", "token", "amount"},
				[]string{"liquidityProvider", "protocolAdapter", "receiver", "token", "amount"},
			)); err != nil {
				return err
			}
		}
		if raw, ok := fields["wrappers"]; ok && !isJSONNull(raw) {
			if err := validateWrappers(raw, fmt.Sprintf("order %d wrappers", index)); err != nil {
				return err
			}
		}
	}

	var liquidity []json.RawMessage
	if err := json.Unmarshal(top["liquidity"], &liquidity); err != nil {
		return fmt.Errorf("liquidity: %w", err)
	}
	for index, raw := range liquidity {
		if err := validateLiquidity(raw, index); err != nil {
			return err
		}
	}
	return nil
}

func validateLiquidity(raw json.RawMessage, index int) error {
	var header struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return fmt.Errorf("liquidity %d: %w", index, err)
	}
	label := fmt.Sprintf("liquidity %d (%s)", index, header.Kind)
	base := []string{"kind", "id", "address", "gasEstimate"}
	var current objectRule
	switch header.Kind {
	case "constantProduct":
		current = rule(append(base, "tokens", "fee", "router"), append(base, "tokens", "fee", "router"))
	case "concentratedLiquidity":
		current = rule(
			append(base, "tokens", "sqrtPrice", "liquidity", "tick", "liquidityNet", "fee", "router"),
			append(base, "tokens", "sqrtPrice", "liquidity", "tick", "liquidityNet", "fee", "router"),
		)
	case "stable":
		current = rule(
			append(base, "tokens", "amplificationParameter", "fee", "balancerPoolId"),
			append(base, "tokens", "amplificationParameter", "fee", "balancerPoolId"),
		)
	case "weightedProduct":
		current = rule(
			append(base, "tokens", "fee", "version", "balancerPoolId"),
			append(base, "tokens", "fee", "version", "balancerPoolId"),
		)
	case "limitOrder":
		current = rule(
			append(base, "hash", "makerToken", "takerToken", "makerAmount", "takerAmount", "takerTokenFeeAmount"),
			append(base, "hash", "makerToken", "takerToken", "makerAmount", "takerAmount", "takerTokenFeeAmount"),
		)
	default:
		return fmt.Errorf("%s: unreviewed liquidity kind", label)
	}
	fields, err := validateObject(raw, label, current)
	if err != nil {
		return err
	}

	switch header.Kind {
	case "constantProduct":
		return validateReserveMap(fields["tokens"], label+" tokens", rule([]string{"balance"}, []string{"balance"}))
	case "stable":
		return validateReserveMap(fields["tokens"], label+" tokens", rule(
			[]string{"balance", "scalingFactor"}, []string{"balance", "scalingFactor"},
		))
	case "weightedProduct":
		return validateReserveMap(fields["tokens"], label+" tokens", rule(
			[]string{"balance", "scalingFactor", "weight"}, []string{"balance", "scalingFactor", "weight"},
		))
	case "concentratedLiquidity":
		var tokens []string
		if err := json.Unmarshal(fields["tokens"], &tokens); err != nil {
			return fmt.Errorf("%s tokens: %w", label, err)
		}
		var liquidityNet map[string]string
		if err := json.Unmarshal(fields["liquidityNet"], &liquidityNet); err != nil {
			return fmt.Errorf("%s liquidityNet: %w", label, err)
		}
	}
	return nil
}

func validateReserveMap(raw json.RawMessage, label string, current objectRule) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for token, reserve := range values {
		if _, err := validateObject(reserve, label+" "+token, current); err != nil {
			return err
		}
	}
	return nil
}

func validateInteractionDataArray(raw json.RawMessage, label string) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for index, value := range values {
		if _, err := validateObject(value, fmt.Sprintf("%s[%d]", label, index), rule(
			[]string{"target", "value", "callData"}, []string{"target", "value", "callData"},
		)); err != nil {
			return err
		}
	}
	return nil
}

func validateWrappers(raw json.RawMessage, label string) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for index, value := range values {
		if _, err := validateObject(value, fmt.Sprintf("%s[%d]", label, index), rule(
			[]string{"address", "data", "isOmittable"}, []string{"address", "data"},
		)); err != nil {
			return err
		}
	}
	return nil
}

func validateFeePolicies(raw json.RawMessage, label string) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for index, value := range values {
		var header struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(value, &header); err != nil {
			return fmt.Errorf("%s[%d]: %w", label, index, err)
		}
		var current objectRule
		switch header.Kind {
		case "surplus":
			current = rule([]string{"kind", "factor", "maxVolumeFactor"}, []string{"kind", "factor", "maxVolumeFactor"})
		case "priceImprovement":
			current = rule([]string{"kind", "factor", "maxVolumeFactor", "quote"}, []string{"kind", "factor", "maxVolumeFactor", "quote"})
		case "volume":
			current = rule([]string{"kind", "factor"}, []string{"kind", "factor"})
		default:
			return fmt.Errorf("%s[%d]: unreviewed fee policy %q", label, index, header.Kind)
		}
		fields, err := validateObject(value, fmt.Sprintf("%s[%d]", label, index), current)
		if err != nil {
			return err
		}
		if header.Kind == "priceImprovement" {
			if _, err := validateObject(fields["quote"], fmt.Sprintf("%s[%d] quote", label, index), rule(
				[]string{"sellAmount", "buyAmount", "fee"}, []string{"sellAmount", "buyAmount", "fee"},
			)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateObject(raw []byte, label string, current objectRule) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s: expected object", label)
	}
	for key := range fields {
		if _, ok := current.allowed[key]; !ok {
			return nil, fmt.Errorf("%s: unreviewed field %q", label, key)
		}
	}
	for key := range current.required {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("%s: missing required field %q", label, key)
		}
	}
	return fields, nil
}

func isJSONNull(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func validHexBytes(value string, size int) bool {
	if !strings.HasPrefix(value, "0x") || len(value) != 2+size*2 {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func validHex(value string) bool {
	if !strings.HasPrefix(value, "0x") || len(value)%2 != 0 {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func validAddress(value string) bool  { return validHexBytes(value, 20) }
func validOrderUID(value string) bool { return validHexBytes(value, 56) }
func validAppData(value string) bool  { return validHexBytes(value, 32) }

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func requireNonNull(raw json.RawMessage, label string) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return errors.New(label + ": null is not allowed")
	}
	return nil
}
'''
write_text("internal/server/wire_contract.go", wire_contract)

server_go = r'''
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
	"strings"
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
		"solution", notification.SolutionID.String(),
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
	for _, owner := range auction.SurplusCapturingJitOrderOwner {
		if !validAddress(owner) {
			return fmt.Errorf("invalid surplus-capturing owner %q", owner)
		}
	}

	seenTokenAddresses := map[string]struct{}{}
	for address, token := range auction.Tokens {
		normalized := strings.ToLower(address)
		if !validAddress(address) || !validU256(token.AvailableBalance, true) {
			return fmt.Errorf("invalid token %q", address)
		}
		if _, duplicate := seenTokenAddresses[normalized]; duplicate {
			return fmt.Errorf("duplicate token address %q", address)
		}
		seenTokenAddresses[normalized] = struct{}{}
		if token.ReferencePrice != "" && !validU256(token.ReferencePrice, true) {
			return fmt.Errorf("invalid reference price for token %q", address)
		}
		if token.Decimals != nil && (*token.Decimals < 0 || *token.Decimals > 255) {
			return fmt.Errorf("invalid decimals for token %q", address)
		}
	}

	seenOrderUIDs := map[string]struct{}{}
	for index, order := range auction.Orders {
		if !validOrderUID(order.UID) || !validAddress(order.SellToken) || !validAddress(order.BuyToken) ||
			!validAddress(order.Owner) || !validU256(order.SellAmount, false) ||
			!validU256(order.FullSellAmount, false) || !validU256(order.BuyAmount, false) ||
			!validU256(order.FullBuyAmount, false) || order.ValidTo < 0 || order.ValidTo > int64(^uint32(0)) ||
			!oneOf(order.Kind, "sell", "buy") || !oneOf(order.Class, "market", "limit") ||
			!oneOf(order.SellTokenSource, "erc20", "internal", "external") ||
			!oneOf(order.BuyTokenDest, "erc20", "internal") ||
			!oneOf(order.SigningScheme, "eip712", "ethsign", "presign", "eip1271", "ethSign", "preSign") ||
			!validAppData(order.AppData) || !validHex(order.Signature) {
			return fmt.Errorf("invalid order %d", index)
		}
		if order.Receiver != "" && !validAddress(order.Receiver) {
			return fmt.Errorf("invalid receiver for order %d", index)
		}
		uid := strings.ToLower(order.UID)
		if _, duplicate := seenOrderUIDs[uid]; duplicate {
			return fmt.Errorf("duplicate order uid %q", order.UID)
		}
		seenOrderUIDs[uid] = struct{}{}
	}

	seenLiquidityIDs := map[string]struct{}{}
	for index, liquidity := range auction.Liquidity {
		if liquidity.ID == "" || !validAddress(liquidity.Address) || !validU256(liquidity.GasEstimate, true) {
			return fmt.Errorf("invalid liquidity at index %d", index)
		}
		if _, duplicate := seenLiquidityIDs[liquidity.ID]; duplicate {
			return fmt.Errorf("duplicate liquidity id %q", liquidity.ID)
		}
		seenLiquidityIDs[liquidity.ID] = struct{}{}
		switch liquidity.Kind {
		case "constantProduct", "concentratedLiquidity":
			if !validAddress(liquidity.Router) {
				return fmt.Errorf("invalid router at liquidity index %d", index)
			}
		case "stable":
			if !validHexBytes(liquidity.BalancerPoolID, 32) {
				return fmt.Errorf("invalid Balancer pool id at liquidity index %d", index)
			}
		case "weightedProduct":
			if !validHexBytes(liquidity.BalancerPoolID, 32) || !oneOf(liquidity.Version, "v0", "v3Plus") {
				return fmt.Errorf("invalid weighted pool at liquidity index %d", index)
			}
		case "limitOrder":
			if !validHexBytes(liquidity.Hash, 32) || !validAddress(liquidity.MakerToken) ||
				!validAddress(liquidity.TakerToken) || !validU256(liquidity.MakerAmount, false) ||
				!validU256(liquidity.TakerAmount, false) || !validU256(liquidity.TakerTokenFeeAmount, true) {
				return fmt.Errorf("invalid limit-order liquidity at index %d", index)
			}
		default:
			return fmt.Errorf("unreviewed liquidity kind %q", liquidity.Kind)
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
	if _, ok := target.(*api.Auction); ok {
		if err := validatePinnedAuctionJSON(data); err != nil {
			return err
		}
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
'''
write_text("internal/server/server.go", server_go)

# Safely skip flashloan semantics instead of silently ignoring them.
solve_path = ROOT / "internal/solve/solve.go"
solve_text = solve_path.read_text(encoding="utf-8")
solve_text = solve_text.replace('import (\n\t"context"', 'import (\n\t"bytes"\n\t"context"', 1)
old = '''\t\tif len(order.PreInteractions) > 0 || len(order.PostInteractions) > 0 ||
\t\t\tlen(order.Wrappers) > 0 || len(order.FeePolicies) > 0 {'''
new = '''\t\tif len(order.PreInteractions) > 0 || len(order.PostInteractions) > 0 ||
\t\t\tlen(order.Wrappers) > 0 || len(order.FeePolicies) > 0 || hasJSONValue(order.FlashloanHint) {'''
if old not in solve_text:
    raise SystemExit("eligible-order marker not found")
solve_text = solve_text.replace(old, new, 1)
marker = 'func eligible(orders []api.Order, max int) ([]api.Order, int) {'
helper = '''func hasJSONValue(raw []byte) bool {
\ttrimmed := bytes.TrimSpace(raw)
\treturn len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

'''
if marker not in solve_text:
    raise SystemExit("eligible function not found")
solve_text = solve_text.replace(marker, helper + marker, 1)
solve_path.write_text(solve_text, encoding="utf-8")

contract_go = r'''
// Package contract verifies the pinned, offline CoW Solver Engine wire contract.
package contract

import (
	"bytes"
	"crypto/sha1" // Git object IDs for the pinned upstream repository are SHA-1.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	Schema     = "aladdin-cow-wire-contract/v1"
	Repository = "cowprotocol/services"
	Commit     = "20b3a62f222ad278502fb7e85cae4938e7f26f65"
)

var expectedSources = map[string]string{
	"crates/solvers/openapi.yml":                                      "64a2466292446ea5f637c809f754fb4a31211a16",
	"crates/driver/src/infra/solver/dto/auction.rs":                    "f857f86838ce8a2a0b9ab0c7185e23eb4c8bcb9f",
	"crates/solvers-dto/src/auction.rs":                               "6c82fd4e461a32d73453feb68d79686642f802d6",
	"crates/solvers-dto/src/solution.rs":                              "816486e47ba0ac8d19da8a31ee722c103ee6c416",
	"crates/liquidity-sources/src/balancer_v2/swap/stable_math.rs":     "3d181998518804abe621f739c033f0e0d75d9dd1",
}

type Source struct {
	Path string `json:"path"`
	Blob string `json:"blob"`
}

type Manifest struct {
	Schema   string `json:"schema"`
	Upstream struct {
		Repository string   `json:"repository"`
		Commit     string   `json:"commit"`
		Sources    []Source `json:"sources"`
	} `json:"upstream"`
	Wire struct {
		AuctionLiquidityID     string `json:"auctionLiquidityId"`
		SolutionLiquidityID    string `json:"solutionLiquidityId"`
		Internalize            string `json:"internalize"`
		NotificationExtensions string `json:"notificationExtensions"`
		SemanticUnknownFields  string `json:"semanticUnknownFields"`
	} `json:"wire"`
	Fixtures map[string]string `json:"fixtures"`
}

func Load(root string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(filepath.Join(root, "contract", "cow-v1", "manifest.json"))
	if err != nil {
		return manifest, fmt.Errorf("read manifest: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode manifest: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func Verify(root string) error {
	manifest, err := Load(root)
	if err != nil {
		return err
	}
	if manifest.Schema != Schema || manifest.Upstream.Repository != Repository || manifest.Upstream.Commit != Commit {
		return errors.New("manifest identity does not match the accepted contract")
	}
	if manifest.Wire.AuctionLiquidityID != "opaque string" ||
		manifest.Wire.SolutionLiquidityID != "opaque string" ||
		manifest.Wire.Internalize != "required boolean" ||
		manifest.Wire.NotificationExtensions != "preserve raw JSON" ||
		manifest.Wire.SemanticUnknownFields != "reject until reviewed" {
		return errors.New("wire policy changed without review")
	}

	seen := map[string]string{}
	for _, source := range manifest.Upstream.Sources {
		if _, duplicate := seen[source.Path]; duplicate {
			return fmt.Errorf("duplicate upstream source %q", source.Path)
		}
		seen[source.Path] = source.Blob
	}
	if len(seen) != len(expectedSources) {
		return fmt.Errorf("source count %d, want %d", len(seen), len(expectedSources))
	}
	for path, blob := range expectedSources {
		if seen[path] != blob {
			return fmt.Errorf("source %s has blob %s, want %s", path, seen[path], blob)
		}
	}

	upstreamRecord, err := os.ReadFile(filepath.Join(root, "UPSTREAM.md"))
	if err != nil {
		return fmt.Errorf("read UPSTREAM.md: %w", err)
	}
	for _, required := range append([]string{Repository, Commit}, sourceStrings()...) {
		if !bytes.Contains(upstreamRecord, []byte(required)) {
			return fmt.Errorf("UPSTREAM.md is missing %q", required)
		}
	}

	if len(manifest.Fixtures) == 0 {
		return errors.New("manifest has no fixtures")
	}
	for name, expected := range manifest.Fixtures {
		if filepath.IsAbs(name) || filepath.Clean(name) != name || strings.HasPrefix(name, "..") {
			return fmt.Errorf("unsafe fixture path %q", name)
		}
		path := filepath.Join(root, "contract", "cow-v1", name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read fixture %s: %w", name, err)
		}
		canonical, err := CanonicalJSON(data)
		if err != nil {
			return fmt.Errorf("canonicalize fixture %s: %w", name, err)
		}
		if !bytes.Equal(data, canonical) {
			return fmt.Errorf("fixture %s is not canonical JSON", name)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expected {
			return fmt.Errorf("fixture %s digest mismatch", name)
		}
	}
	return nil
}

func VerifyUpstreamDirectory(directory string) error {
	for path, expected := range expectedSources {
		data, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("read upstream %s: %w", path, err)
		}
		if got := GitBlobSHA(data); got != expected {
			return fmt.Errorf("upstream drift at %s: blob %s, want %s", path, got, expected)
		}
	}
	return nil
}

func GitBlobSHA(data []byte) string {
	hash := sha1.New()
	_, _ = fmt.Fprintf(hash, "blob %d%c", len(data), byte(0))
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func CanonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func sourceStrings() []string {
	values := make([]string, 0, len(expectedSources)*2)
	for path, blob := range expectedSources {
		values = append(values, path, blob)
	}
	return values
}
'''
write_text("internal/contract/contract.go", contract_go)

contract_test = r'''
package contract

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPinnedContractVerifies(t *testing.T) {
	if err := Verify(repositoryRoot(t)); err != nil {
		t.Fatal(err)
	}
}

func TestGitBlobSHA(t *testing.T) {
	const want = "ce013625030ba8dba906f756967f9e9ca394464a"
	if got := GitBlobSHA([]byte("hello\n")); got != want {
		t.Fatalf("blob SHA = %s, want %s", got, want)
	}
}

func TestVerifyRejectsFixtureMutation(t *testing.T) {
	source := repositoryRoot(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "UPSTREAM.md"), mustRead(t, filepath.Join(source, "UPSTREAM.md")), 0o600); err != nil {
		t.Fatal(err)
	}
	contractSource := filepath.Join(source, "contract", "cow-v1")
	contractTarget := filepath.Join(target, "contract", "cow-v1")
	if err := filepath.WalkDir(contractSource, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(contractSource, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(contractTarget, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		return os.WriteFile(destination, mustRead(t, path), 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(contractTarget, "notification.json")
	file, err := os.OpenFile(fixture, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(" ")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(errors.Join(writeErr, closeErr))
	}
	if err := Verify(target); err == nil {
		t.Fatal("tampered fixture was accepted")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
'''
write_text("internal/contract/contract_test.go", contract_test)

contractcheck = r'''
// Command contractcheck verifies the pinned offline CoW wire contract.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
)

func main() {
	root := flag.String("root", ".", "repository root")
	upstream := flag.String("upstream-dir", "", "optional cowprotocol/services checkout to verify")
	flag.Parse()
	if err := contract.Verify(*root); err != nil {
		fmt.Fprintln(os.Stderr, "contract verification:", err)
		os.Exit(1)
	}
	if *upstream != "" {
		if err := contract.VerifyUpstreamDirectory(*upstream); err != nil {
			fmt.Fprintln(os.Stderr, "upstream verification:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("%s verified at %s\n", contract.Schema, contract.Commit)
}
'''
write_text("cmd/contractcheck/main.go", contractcheck)

vector_generator = r'''
#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TARGET = ROOT / "contract" / "cow-v1" / "reference-vectors.json"
ONE = 10**18
AMP_PRECISION = 1000


def ceil_div(a: int, b: int) -> int:
    if b <= 0:
        raise ValueError("division by zero")
    return (a + b - 1) // b


def constant_product(amount: int, reserve_in: int, reserve_out: int, fee_num: int, fee_den: int) -> int:
    adjusted = amount * (fee_den - fee_num)
    return adjusted * reserve_out // (reserve_in * fee_den + adjusted)


def concentrated() -> dict[str, object]:
    q96 = 1 << 96
    liquidity = 10**20
    sqrt_price = q96
    amount = 10**18
    fee_pips = 3000
    less_fee = amount * (1_000_000 - fee_pips) // 1_000_000
    numerator = liquidity << 96
    next_sqrt = ceil_div(numerator * sqrt_price, numerator + less_fee * sqrt_price)
    intermediate = ceil_div(numerator * (sqrt_price - next_sqrt), sqrt_price)
    step_in = ceil_div(intermediate, next_sqrt)
    output = liquidity * (sqrt_price - next_sqrt) // q96
    assert step_in <= less_fee and output > 0
    return {
        "amountIn": str(amount),
        "expectedOut": str(output),
        "feeDen": "1000",
        "feeNum": "3",
        "liquidity": str(liquidity),
        "sqrtPriceX96": str(sqrt_price),
        "tick": 0,
        "ticks": [],
        "tokenA": "0x0a",
        "tokenB": "0x0b",
        "tokenIn": "0x0a",
        "tokenOut": "0x0b",
    }


def stable_invariant(amp: int, balances: list[int]) -> int:
    n = len(balances)
    total = sum(balances)
    if total == 0:
        return 0
    invariant = total
    amp_total = amp * n
    for _ in range(255):
        d_p = invariant
        for balance in balances:
            d_p = d_p * invariant // (balance * n)
        previous = invariant
        numerator = ((amp_total * total // AMP_PRECISION) + d_p * n) * invariant
        denominator = ((amp_total - AMP_PRECISION) * invariant // AMP_PRECISION) + (n + 1) * d_p
        invariant = numerator // denominator
        if abs(invariant - previous) <= 1:
            return invariant
    raise RuntimeError("invariant did not converge")


def stable_balance(amp: int, balances: list[int], invariant: int, token_index: int) -> int:
    n = len(balances)
    amp_total = amp * n
    total = balances[0]
    p_d = total * n
    for balance in balances[1:]:
        p_d = p_d * balance * n // invariant
        total += balance
    total -= balances[token_index]
    inv2 = invariant * invariant
    c = ceil_div(inv2, amp_total * p_d) * AMP_PRECISION * balances[token_index]
    b = (invariant // amp_total) * AMP_PRECISION + total
    token_balance = ceil_div(inv2 + c, invariant + b)
    for _ in range(255):
        previous = token_balance
        token_balance = ceil_div(token_balance * token_balance + c, token_balance * 2 + b - invariant)
        if abs(token_balance - previous) <= 1:
            return token_balance
    raise RuntimeError("balance did not converge")


def stable() -> dict[str, object]:
    token_list = ["0xa", "0xb"]
    balances = [10**24, 10**24]
    scaling = [ONE, ONE]
    amp = 100_000
    fee_num, fee_den = 4, 10_000
    amount = 10**18
    fee_bfp = fee_num * ONE // fee_den
    fee = ceil_div(amount * fee_bfp, ONE)
    after_fee = amount - fee
    upscaled = [balance * scale // ONE for balance, scale in zip(balances, scaling)]
    up_input = after_fee * scaling[0] // ONE
    invariant = stable_invariant(amp, upscaled)
    changed = list(upscaled)
    changed[0] += up_input
    final_out = stable_balance(amp, changed, invariant, 1)
    up_out = upscaled[1] - final_out - 1
    output = up_out * ONE // scaling[1]
    return {
        "amountIn": str(amount),
        "amplificationRaw": str(amp),
        "balances": [str(value) for value in balances],
        "expectedOut": str(output),
        "feeDen": str(fee_den),
        "feeNum": str(fee_num),
        "scalingFactors": [str(value) for value in scaling],
        "tokenIn": "0xa",
        "tokenList": token_list,
        "tokenOut": "0xb",
    }


def document() -> dict[str, object]:
    return {
        "concentratedLiquidity": [concentrated()],
        "constantProduct": [
            {
                "amountIn": "1000",
                "expectedOut": "996",
                "feeDen": "1000",
                "feeNum": "3",
                "reserveIn": "1000000",
                "reserveOut": "1000000",
                "tokenIn": "0xa",
                "tokenOut": "0xb",
            },
            {
                "amountIn": "1000000000000000000",
                "expectedOut": str(constant_product(10**18, 10**20, 2 * 10**23, 3, 1000)),
                "feeDen": "1000",
                "feeNum": "3",
                "reserveIn": str(10**20),
                "reserveOut": str(2 * 10**23),
                "tokenIn": "0xa",
                "tokenOut": "0xb",
            },
        ],
        "schema": "aladdin-cross-language-amm-vectors/v1",
        "stable": [stable()],
        "tickMath": [
            {"expectedSqrtPriceX96": "4295128739", "tick": -887272},
            {"expectedSqrtPriceX96": "79224201403219477170569942574", "tick": -1},
            {"expectedSqrtPriceX96": "79228162514264337593543950336", "tick": 0},
            {"expectedSqrtPriceX96": "79232123823359799118286999568", "tick": 1},
            {"expectedSqrtPriceX96": "1461446703485210103287273052203988822378723970342", "tick": 887272},
        ],
    }


def encoded() -> bytes:
    return (json.dumps(document(), sort_keys=True, separators=(",", ":")) + "\n").encode()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    expected = encoded()
    if args.check:
        actual = TARGET.read_bytes()
        if actual != expected:
            raise SystemExit("reference-vectors.json is stale; run scripts/generate_reference_vectors.py")
        return
    TARGET.parent.mkdir(parents=True, exist_ok=True)
    TARGET.write_bytes(expected)


if __name__ == "__main__":
    main()
'''
write_text("scripts/generate_reference_vectors.py", vector_generator)
subprocess.run([sys.executable, str(ROOT / "scripts/generate_reference_vectors.py")], check=True)

amm_vectors_test = r'''
package amm

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

type contractVectors struct {
	Schema          string `json:"schema"`
	ConstantProduct []struct {
		TokenIn, TokenOut, AmountIn, ReserveIn, ReserveOut, FeeNum, FeeDen, ExpectedOut string
	} `json:"constantProduct"`
	ConcentratedLiquidity []struct {
		TokenA, TokenB, TokenIn, TokenOut, AmountIn, SqrtPriceX96, Liquidity, FeeNum, FeeDen, ExpectedOut string
		Tick                                                                                              int32
		Ticks                                                                                             []struct {
			Index int32  `json:"index"`
			Net   string `json:"net"`
		} `json:"ticks"`
	} `json:"concentratedLiquidity"`
	Stable []struct {
		TokenList, Balances, ScalingFactors                                      []string
		TokenIn, TokenOut, AmountIn, AmplificationRaw, FeeNum, FeeDen, ExpectedOut string
	} `json:"stable"`
	TickMath []struct {
		Tick                 int32  `json:"tick"`
		ExpectedSqrtPriceX96 string `json:"expectedSqrtPriceX96"`
	} `json:"tickMath"`
}

func TestPinnedCrossLanguageVectors(t *testing.T) {
	path := filepath.Join("..", "..", "contract", "cow-v1", "reference-vectors.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vectors contractVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.Schema != "aladdin-cross-language-amm-vectors/v1" {
		t.Fatalf("unexpected vector schema %q", vectors.Schema)
	}

	for _, vector := range vectors.ConstantProduct {
		pool := &Pool{
			Kind: "constantProduct", TokenA: vector.TokenIn, TokenB: vector.TokenOut,
			ReserveA: vectorBig(t, vector.ReserveIn), ReserveB: vectorBig(t, vector.ReserveOut),
			FeeNum: vectorBig(t, vector.FeeNum), FeeDen: vectorBig(t, vector.FeeDen),
		}
		output, err := pool.QuoteExactInPair(vector.TokenIn, vector.TokenOut, vectorBig(t, vector.AmountIn))
		if err != nil {
			t.Fatal(err)
		}
		if output.String() != vector.ExpectedOut {
			t.Fatalf("constant product output %s, want %s", output, vector.ExpectedOut)
		}
	}

	for _, vector := range vectors.ConcentratedLiquidity {
		ticks := make([]Tick, 0, len(vector.Ticks))
		for _, item := range vector.Ticks {
			ticks = append(ticks, Tick{Index: item.Index, Net: vectorBig(t, item.Net)})
		}
		pool := &Pool{
			Kind: "concentratedLiquidity", TokenA: vector.TokenA, TokenB: vector.TokenB,
			SqrtPriceX96: vectorBig(t, vector.SqrtPriceX96), Liquidity: vectorBig(t, vector.Liquidity),
			Tick: vector.Tick, Ticks: ticks, FeeNum: vectorBig(t, vector.FeeNum), FeeDen: vectorBig(t, vector.FeeDen),
		}
		output, err := pool.QuoteExactInPair(vector.TokenIn, vector.TokenOut, vectorBig(t, vector.AmountIn))
		if err != nil {
			t.Fatal(err)
		}
		if output.String() != vector.ExpectedOut {
			t.Fatalf("concentrated output %s, want %s", output, vector.ExpectedOut)
		}
	}

	for _, vector := range vectors.Stable {
		balances := make([]*big.Int, len(vector.Balances))
		scaling := make([]*big.Int, len(vector.ScalingFactors))
		for index := range balances {
			balances[index] = vectorBig(t, vector.Balances[index])
			scaling[index] = vectorBig(t, vector.ScalingFactors[index])
		}
		pool := &Pool{
			Kind: "stable", TokenA: vector.TokenList[0], TokenB: vector.TokenList[1], TokenList: vector.TokenList,
			Balances: balances, ScalingFactors: scaling, AmplificationRaw: vectorBig(t, vector.AmplificationRaw),
			FeeNum: vectorBig(t, vector.FeeNum), FeeDen: vectorBig(t, vector.FeeDen),
		}
		output, err := pool.QuoteExactInPair(vector.TokenIn, vector.TokenOut, vectorBig(t, vector.AmountIn))
		if err != nil {
			t.Fatal(err)
		}
		if output.String() != vector.ExpectedOut {
			t.Fatalf("stable output %s, want %s", output, vector.ExpectedOut)
		}
	}

	for _, vector := range vectors.TickMath {
		value, err := GetSqrtRatioAtTick(vector.Tick)
		if err != nil {
			t.Fatal(err)
		}
		if value.String() != vector.ExpectedSqrtPriceX96 {
			t.Fatalf("tick %d output %s, want %s", vector.Tick, value, vector.ExpectedSqrtPriceX96)
		}
	}
}

func vectorBig(t *testing.T, value string) *big.Int {
	t.Helper()
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok {
		t.Fatalf("invalid vector integer %q", value)
	}
	return parsed
}
'''
write_text("internal/amm/contract_vectors_test.go", amm_vectors_test)

# Representative pinned fixtures.
addresses = [f"0x{value:040x}" for value in range(10, 16)]
A, B, C, D, E, F = addresses
owner = "0x0000000000000000000000000000000000000001"
receiver = "0x0000000000000000000000000000000000000002"
uid1 = "0x" + "11" * 56
uid2 = "0x" + "22" * 56
app1 = "0x" + "33" * 32
app2 = "0x" + "44" * 32
sig = "0x" + "55" * 65
balancer1 = "0x" + "66" * 32
balancer2 = "0x" + "77" * 32
limit_hash = "0x" + "88" * 32

auction = {
    "deadline": "2100-01-01T00:00:00Z",
    "effectiveGasPrice": "1000000000",
    "id": "4242",
    "liquidity": [
        {
            "address": "0x0000000000000000000000000000000000000101",
            "fee": "0.003",
            "gasEstimate": "90000",
            "id": "cp:7",
            "kind": "constantProduct",
            "router": "0x0000000000000000000000000000000000000201",
            "tokens": {
                A: {"balance": str(10**24)},
                B: {"balance": str(2 * 10**24)},
            },
        },
        {
            "address": "0x0000000000000000000000000000000000000102",
            "fee": "0.003",
            "gasEstimate": "120000",
            "id": "v3:8",
            "kind": "concentratedLiquidity",
            "liquidity": str(10**20),
            "liquidityNet": {"-60000": str(10**20), "60000": str(-(10**20))},
            "router": "0x0000000000000000000000000000000000000202",
            "sqrtPrice": str(1 << 96),
            "tick": 0,
            "tokens": [C, D],
        },
        {
            "address": "0x0000000000000000000000000000000000000103",
            "amplificationParameter": "100",
            "balancerPoolId": balancer1,
            "fee": "0.0004",
            "gasEstimate": "150000",
            "id": "stable:9",
            "kind": "stable",
            "tokens": {
                D: {"balance": str(10**24), "scalingFactor": "1"},
                E: {"balance": str(10**24), "scalingFactor": "1"},
            },
        },
        {
            "address": "0x0000000000000000000000000000000000000104",
            "balancerPoolId": balancer2,
            "fee": "0.001",
            "gasEstimate": "160000",
            "id": "weighted:10",
            "kind": "weightedProduct",
            "tokens": {
                E: {"balance": str(10**24), "scalingFactor": "1", "weight": "0.5"},
                F: {"balance": str(10**24), "scalingFactor": "1", "weight": "0.5"},
            },
            "version": "v0",
        },
        {
            "address": "0x0000000000000000000000000000000000000105",
            "gasEstimate": "130000",
            "hash": limit_hash,
            "id": "limit:11",
            "kind": "limitOrder",
            "makerAmount": "1000",
            "makerToken": F,
            "takerAmount": "900",
            "takerToken": A,
            "takerTokenFeeAmount": "0",
        },
    ],
    "orders": [
        {
            "appData": app1,
            "buyAmount": "1",
            "buyToken": B,
            "buyTokenDestination": "erc20",
            "class": "market",
            "fullBuyAmount": "1",
            "fullSellAmount": str(10**18),
            "kind": "sell",
            "owner": owner,
            "partiallyFillable": False,
            "postInteractions": [],
            "preInteractions": [],
            "receiver": receiver,
            "sellAmount": str(10**18),
            "sellToken": A,
            "sellTokenSource": "erc20",
            "signature": sig,
            "signingScheme": "eip712",
            "uid": uid1,
            "validTo": 4102444800,
        },
        {
            "appData": app2,
            "buyAmount": "900",
            "buyToken": D,
            "buyTokenDestination": "erc20",
            "class": "limit",
            "feePolicies": [
                {
                    "factor": 0.5,
                    "kind": "priceImprovement",
                    "maxVolumeFactor": 0.01,
                    "quote": {"buyAmount": "900", "fee": "0", "sellAmount": "1000"},
                }
            ],
            "flashloanHint": {
                "amount": "1000",
                "liquidityProvider": "0x0000000000000000000000000000000000000301",
                "protocolAdapter": "0x0000000000000000000000000000000000000302",
                "receiver": "0x0000000000000000000000000000000000000303",
                "token": C,
            },
            "fullBuyAmount": "900",
            "fullSellAmount": "1000",
            "kind": "sell",
            "owner": owner,
            "partiallyFillable": False,
            "postInteractions": [],
            "preInteractions": [
                {"callData": "0x", "target": "0x0000000000000000000000000000000000000401", "value": "0"}
            ],
            "sellAmount": "1000",
            "sellToken": C,
            "sellTokenSource": "erc20",
            "signature": sig,
            "signingScheme": "eip712",
            "uid": uid2,
            "validTo": 4102444800,
            "wrappers": [
                {"address": "0x0000000000000000000000000000000000000501", "data": "0x", "isOmittable": True}
            ],
        },
    ],
    "surplusCapturingJitOrderOwners": [owner],
    "tokens": {
        address: {
            "availableBalance": "0",
            "decimals": 18,
            "referencePrice": "1000000000000000000",
            "symbol": f"T{index}",
            "trusted": True,
        }
        for index, address in enumerate(addresses)
    },
}
write_json("contract/cow-v1/auction.json", auction)

amount = 10**18
reserve_in = 10**24
reserve_out = 2 * 10**24
adjusted = amount * 997
output = adjusted * reserve_out // (reserve_in * 1000 + adjusted)
solution = {
    "solutions": [
        {
            "gas": 256000,
            "id": 0,
            "interactions": [
                {
                    "id": "cp:7",
                    "inputAmount": str(amount),
                    "inputToken": A,
                    "internalize": False,
                    "kind": "liquidity",
                    "outputAmount": str(output),
                    "outputToken": B,
                }
            ],
            "prices": {A: str(output), B: str(amount)},
            "trades": [{"executedAmount": str(amount), "kind": "fulfillment", "order": uid1}],
        }
    ]
}
write_json("contract/cow-v1/solution.json", solution)

notification = {
    "auctionId": "4242",
    "details": {"objective": "123456789012345678901234567890", "winner": False},
    "kind": "success",
    "rank": 2,
    "solutionId": 9007199254740993,
}
write_json("contract/cow-v1/notification.json", notification)

manifest_sources = [
    {"blob": "64a2466292446ea5f637c809f754fb4a31211a16", "path": "crates/solvers/openapi.yml"},
    {"blob": "f857f86838ce8a2a0b9ab0c7185e23eb4c8bcb9f", "path": "crates/driver/src/infra/solver/dto/auction.rs"},
    {"blob": "6c82fd4e461a32d73453feb68d79686642f802d6", "path": "crates/solvers-dto/src/auction.rs"},
    {"blob": "816486e47ba0ac8d19da8a31ee722c103ee6c416", "path": "crates/solvers-dto/src/solution.rs"},
    {"blob": "3d181998518804abe621f739c033f0e0d75d9dd1", "path": "crates/liquidity-sources/src/balancer_v2/swap/stable_math.rs"},
]
fixture_names = ["auction.json", "notification.json", "reference-vectors.json", "solution.json"]
fixture_hashes = {
    name: hashlib.sha256((ROOT / "contract" / "cow-v1" / name).read_bytes()).hexdigest()
    for name in fixture_names
}
manifest = {
    "fixtures": fixture_hashes,
    "schema": "aladdin-cow-wire-contract/v1",
    "upstream": {
        "commit": "20b3a62f222ad278502fb7e85cae4938e7f26f65",
        "repository": "cowprotocol/services",
        "sources": manifest_sources,
    },
    "wire": {
        "auctionLiquidityId": "opaque string",
        "internalize": "required boolean",
        "notificationExtensions": "preserve raw JSON",
        "semanticUnknownFields": "reject until reviewed",
        "solutionLiquidityId": "opaque string",
    },
}
write_json("contract/cow-v1/manifest.json", manifest)

api_test = r'''
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedAuctionRoundTrip(t *testing.T) {
	data := fixture(t, "auction.json")
	var auction Auction
	if err := json.Unmarshal(data, &auction); err != nil {
		t.Fatal(err)
	}
	if len(auction.Orders) != 2 || len(auction.Liquidity) != 5 {
		t.Fatalf("fixture inventory changed: orders=%d liquidity=%d", len(auction.Orders), len(auction.Liquidity))
	}
	if auction.Orders[1].FeePolicies[0].Quote == nil || auction.Orders[1].FeePolicies[0].Quote.SellAmount != "1000" {
		t.Fatal("price-improvement quote was not preserved")
	}
	if len(auction.Orders[1].FlashloanHint) == 0 || len(auction.Orders[1].Wrappers) != 1 {
		t.Fatal("unsupported order semantics were silently lost")
	}
	if auction.Liquidity[2].BalancerPoolID == "" || auction.Liquidity[3].Version != "v0" ||
		auction.Liquidity[4].Hash == "" || auction.Liquidity[4].MakerAmount != "1000" {
		t.Fatal("pinned liquidity union fields were not preserved")
	}
	encoded, err := json.Marshal(auction)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalEqual(t, data, encoded)
}

func TestPinnedSolutionUsesRuntimeDTOShape(t *testing.T) {
	data := fixture(t, "solution.json")
	var response SolveResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Solutions) != 1 || len(response.Solutions[0].Interactions) != 1 {
		t.Fatal("solution fixture inventory changed")
	}
	interaction := response.Solutions[0].Interactions[0]
	if interaction.ID != "cp:7" || interaction.Internalize {
		t.Fatalf("runtime interaction shape changed: %+v", interaction)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalEqual(t, data, encoded)
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	item := wire["solutions"].([]any)[0].(map[string]any)["interactions"].([]any)[0].(map[string]any)
	if _, ok := item["id"].(string); !ok {
		t.Fatalf("liquidity id is not a JSON string: %#v", item["id"])
	}
	if value, ok := item["internalize"].(bool); !ok || value {
		t.Fatalf("internalize=false was omitted or changed: %#v", item["internalize"])
	}
}

func TestPinnedNotificationPreservesLargeIDAndExtensions(t *testing.T) {
	data := fixture(t, "notification.json")
	var notification Notification
	if err := json.Unmarshal(data, &notification); err != nil {
		t.Fatal(err)
	}
	if notification.SolutionID.String() != "9007199254740993" {
		t.Fatalf("solution ID lost precision: %s", notification.SolutionID)
	}
	if len(notification.Extra) != 2 {
		t.Fatalf("extension count = %d", len(notification.Extra))
	}
	encoded, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalEqual(t, data, encoded)
}

func TestNotificationUnmarshalReplacesPreviousState(t *testing.T) {
	notification := Notification{
		AuctionID: "old", SolutionID: json.Number("9"), Kind: "old",
		Extra: map[string]json.RawMessage{"stale": json.RawMessage(`true`)},
	}
	if err := json.Unmarshal([]byte(`{"auctionId":"new","solutionId":1,"kind":"success"}`), &notification); err != nil {
		t.Fatal(err)
	}
	if notification.AuctionID != "new" || notification.SolutionID.String() != "1" || notification.Kind != "success" {
		t.Fatalf("old core state survived unmarshal: %+v", notification)
	}
	if _, ok := notification.Extra["stale"]; ok {
		t.Fatalf("old extension state survived unmarshal: %+v", notification.Extra)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "contract", "cow-v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertCanonicalEqual(t *testing.T, left, right []byte) {
	t.Helper()
	leftCanonical, err := canonicalJSON(left)
	if err != nil {
		t.Fatal(err)
	}
	rightCanonical, err := canonicalJSON(right)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftCanonical, rightCanonical) {
		t.Fatalf("canonical JSON differs\nleft:  %s\nright: %s", leftCanonical, rightCanonical)
	}
}

func canonicalJSON(data []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}
'''
write_text("internal/api/types_test.go", api_test)

server_test_path = ROOT / "internal/server/server_test.go"
server_test = server_test_path.read_text(encoding="utf-8")
server_test = server_test.replace('"uid": "0xdeadbeef",', '"uid": "' + uid1 + '",', 1)
server_test = server_test.replace('"sellAmount": "1000000000000000000",\n\t    "buyAmount": "1",', '"sellAmount": "1000000000000000000",\n\t    "fullSellAmount": "1000000000000000000",\n\t    "buyAmount": "1",', 1)
server_test = server_test.replace('"appData": "0x00",', '"appData": "' + app1 + '",', 1)
server_test = server_test.replace('"id": "7",', '"id": "cp:7",', 1)
old_assert = '''\t// The driver expects the liquidity id as a JSON number here, even though
\t// the auction carries it as a string. Getting this wrong is silent failure.
\tif id, ok := s.Interactions[0]["id"].(float64); !ok || id != 7 {
\t\tt.Errorf("interaction id must be the numeric liquidity id 7, got %#v", s.Interactions[0]["id"])
\t}'''
new_assert = '''\tif id, ok := s.Interactions[0]["id"].(string); !ok || id != "cp:7" {
\t\tt.Errorf("interaction id must preserve the opaque string, got %#v", s.Interactions[0]["id"])
\t}
\tif internalize, ok := s.Interactions[0]["internalize"].(bool); !ok || internalize {
\t\tt.Errorf("internalize=false must be emitted explicitly, got %#v", s.Interactions[0]["internalize"])
\t}'''
if old_assert not in server_test:
    raise SystemExit("server interaction assertion marker not found")
server_test = server_test.replace(old_assert, new_assert, 1)
server_test_path.write_text(server_test, encoding="utf-8")

recovery_test = r'''
package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInvalidDeadlineRejected(t *testing.T) {
	response := post(t, testServer(t), "/solve", auctionJSON("not-a-deadline"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid deadline returned %d, want 400", response.Code)
	}
}

func TestTrailingAuctionJSONRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	response := post(t, testServer(t), "/solve", auctionJSON(deadline)+` {}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON returned %d, want 400", response.Code)
	}
}

func TestDuplicateTopLevelFieldRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := strings.Replace(
		auctionJSON(deadline),
		`"effectiveGasPrice": "1000000000"`,
		`"effectiveGasPrice": "1", "effectiveGasPrice": "1000000000"`,
		1,
	)
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate field returned %d, want 400", response.Code)
	}
}

func TestDuplicateNestedFieldRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := strings.Replace(
		auctionJSON(deadline),
		`{"balance": "1000000000000000000000"}`,
		`{"balance":"100","balance":"200"}`,
		1,
	)
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("nested duplicate returned %d, want 400", response.Code)
	}
}

func TestOpaqueLiquidityIDAccepted(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	response := post(t, testServer(t), "/solve", auctionJSON(deadline))
	if response.Code != http.StatusOK {
		t.Fatalf("opaque liquidity id returned %d, want 200", response.Code)
	}
}

func TestMissingRequiredAuctionCollectionRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := `{
		"tokens":{},
		"liquidity":[],
		"effectiveGasPrice":"1",
		"deadline":"` + deadline + `",
		"surplusCapturingJitOrderOwners":[]
	}`
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing orders returned %d, want 400", response.Code)
	}
}
'''
write_text("internal/server/recovery_test.go", recovery_test)

adversarial_test = r'''
package server

import (
	"encoding/json"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func validContractAuction() *api.Auction {
	return &api.Auction{
		Tokens:                        map[string]api.TokenInfo{},
		Orders:                        []api.Order{},
		Liquidity:                     []api.Liquidity{},
		EffectiveGasPrice:             "1",
		SurplusCapturingJitOrderOwner: []string{},
	}
}

func validContractOrder(uid string) api.Order {
	return api.Order{
		UID: uid,
		SellToken: "0x0000000000000000000000000000000000000001",
		BuyToken: "0x0000000000000000000000000000000000000002",
		SellAmount: "1", FullSellAmount: "1", BuyAmount: "1", FullBuyAmount: "1",
		ValidTo: 1, Kind: "sell", Class: "market",
		Owner: "0x0000000000000000000000000000000000000003",
		SellTokenSource: "erc20", BuyTokenDest: "erc20",
		PreInteractions: []json.RawMessage{}, PostInteractions: []json.RawMessage{},
		AppData: "0x0000000000000000000000000000000000000000000000000000000000000000",
		SigningScheme: "eip712", Signature: "0x",
	}
}

func TestLiquidityIDsAreOpaqueNonEmptyAndUnique(t *testing.T) {
	auction := validContractAuction()
	auction.Liquidity = []api.Liquidity{
		{Kind: "constantProduct", ID: "pool:01", Address: "0x0000000000000000000000000000000000000004", GasEstimate: "1", Router: "0x0000000000000000000000000000000000000005"},
		{Kind: "constantProduct", ID: "Pool:01", Address: "0x0000000000000000000000000000000000000006", GasEstimate: "1", Router: "0x0000000000000000000000000000000000000007"},
	}
	if err := validateAuction(auction); err != nil {
		t.Fatalf("case-sensitive opaque IDs were rejected: %v", err)
	}
	auction.Liquidity[1].ID = "pool:01"
	if err := validateAuction(auction); err == nil {
		t.Fatal("duplicate opaque liquidity ID was accepted")
	}
	auction.Liquidity[1].ID = ""
	if err := validateAuction(auction); err == nil {
		t.Fatal("empty liquidity ID was accepted")
	}
}

func TestSemanticDuplicatesRejected(t *testing.T) {
	t.Run("token address", func(t *testing.T) {
		auction := validContractAuction()
		auction.Tokens = map[string]api.TokenInfo{
			"0x00000000000000000000000000000000000000Aa": {AvailableBalance: "0"},
			"0x00000000000000000000000000000000000000aa": {AvailableBalance: "0"},
		}
		if err := validateAuction(auction); err == nil {
			t.Fatal("case-insensitive duplicate token addresses were accepted")
		}
	})

	t.Run("order uid", func(t *testing.T) {
		upper := "0x" + "AB" + string(make([]byte, 0))
		_ = upper
		uidA := "0x1111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111"
		uidB := "0x1111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111"
		auction := validContractAuction()
		auction.Orders = []api.Order{validContractOrder(uidA), validContractOrder(uidB)}
		if err := validateAuction(auction); err == nil {
			t.Fatal("duplicate order UIDs were accepted")
		}
	})
}
'''
# Remove an intentionally unused temporary variable from the generated source.
adversarial_test = adversarial_test.replace('\n\t\tupper := "0x" + "AB" + string(make([]byte, 0))\n\t\t_ = upper', '')
write_text("internal/server/adversarial_test.go", adversarial_test)

phase1_server_test = r'''
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedAuctionFixtureProducesPinnedSolution(t *testing.T) {
	auction := phase1Fixture(t, "auction.json")
	response := post(t, testServer(t), "/solve", string(auction))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body)
	}
	assertPhase1CanonicalEqual(t, phase1Fixture(t, "solution.json"), response.Body.Bytes())
}

func TestUnreviewedSemanticFieldsFailClosed(t *testing.T) {
	base := phase1FixtureMap(t)
	cases := map[string]func(map[string]any){
		"auction": func(value map[string]any) { value["newSettlementMode"] = "unsafe" },
		"token": func(value map[string]any) {
			for _, token := range value["tokens"].(map[string]any) {
				token.(map[string]any)["rebasingMode"] = true
				break
			}
		},
		"order": func(value map[string]any) {
			value["orders"].([]any)[0].(map[string]any)["newExecutionMode"] = "unsafe"
		},
		"supported liquidity": func(value map[string]any) {
			value["liquidity"].([]any)[0].(map[string]any)["newInvariant"] = "unsafe"
		},
		"known unsupported liquidity": func(value map[string]any) {
			value["liquidity"].([]any)[3].(map[string]any)["newWeightRule"] = "unsafe"
		},
		"new liquidity kind": func(value map[string]any) {
			value["liquidity"].([]any)[0].(map[string]any)["kind"] = "newPool"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := deepCopyMap(t, base)
			mutate(value)
			payload, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			response := post(t, testServer(t), "/solve", string(payload))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", response.Code)
			}
		})
	}
}

func TestPinnedNotificationFixtureAccepted(t *testing.T) {
	response := post(t, testServer(t), "/notify", string(phase1Fixture(t, "notification.json")))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", response.Code)
	}
}

func phase1Fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "contract", "cow-v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func phase1FixtureMap(t *testing.T) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(phase1Fixture(t, "auction.json")))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func deepCopyMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var copied map[string]any
	if err := decoder.Decode(&copied); err != nil {
		t.Fatal(err)
	}
	return copied
}

func assertPhase1CanonicalEqual(t *testing.T, left, right []byte) {
	t.Helper()
	canonical := func(data []byte) []byte {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			t.Fatalf("trailing JSON: %v", err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	leftCanonical, rightCanonical := canonical(left), canonical(right)
	if !bytes.Equal(leftCanonical, rightCanonical) {
		t.Fatalf("JSON mismatch\nwant: %s\ngot:  %s", leftCanonical, rightCanonical)
	}
}
'''
write_text("internal/server/phase1_contract_test.go", phase1_server_test)

solve_phase1_test = r'''
package solve

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func TestOrderWithFlashloanHintIsSkipped(t *testing.T) {
	order := sellOrder("0xflashloan", tokA, tokB, "1000", "1")
	order.FlashloanHint = json.RawMessage(`{"amount":"1000"}`)
	auction := &api.Auction{
		Orders: []api.Order{order},
		Liquidity: []api.Liquidity{cpPool("1", tokA, tokB, "1000000", "1000000")},
		EffectiveGasPrice: "1", Tokens: map[string]api.TokenInfo{},
	}
	config := DefaultConfig()
	config.RequireProfitable = false
	result := Solve(context.Background(), auction, config)
	if result.Stats.Orders != 0 || result.Stats.DroppedUnsupportedOrder != 1 || len(result.Solutions) != 0 {
		t.Fatalf("flashloan semantics were not skipped: %+v", result.Stats)
	}
}
'''
write_text("internal/solve/phase1_contract_test.go", solve_phase1_test)

upstream_md = r'''
# Upstream Contract Pin

The shadow engine is reviewed against an exact snapshot of the CoW Protocol
reference implementation. This pin is an offline source-review contract; the
running service performs no network fetch and does not depend on the upstream
repository at runtime.

## Accepted upstream snapshot

- Repository: `cowprotocol/services`
- Commit: `20b3a62f222ad278502fb7e85cae4938e7f26f65`
- Commit date: 2026-08-14
- Pin reviewed: 2026-08-16

Authoritative files used by this repository:

| Purpose | Upstream path | Git blob SHA |
|---|---|---|
| Published Solver Engine OpenAPI | `crates/solvers/openapi.yml` | `64a2466292446ea5f637c809f754fb4a31211a16` |
| Driver auction construction | `crates/driver/src/infra/solver/dto/auction.rs` | `f857f86838ce8a2a0b9ab0c7185e23eb4c8bcb9f` |
| Runtime auction DTO | `crates/solvers-dto/src/auction.rs` | `6c82fd4e461a32d73453feb68d79686642f802d6` |
| Runtime solution DTO | `crates/solvers-dto/src/solution.rs` | `816486e47ba0ac8d19da8a31ee722c103ee6c416` |
| Balancer stable arithmetic | `crates/liquidity-sources/src/balancer_v2/swap/stable_math.rs` | `3d181998518804abe621f739c033f0e0d75d9dd1` |

The machine-readable copy of this identity is
`contract/cow-v1/manifest.json`. `go run ./cmd/contractcheck` verifies its
fixtures, hashes and this record. Supplying `-upstream-dir` additionally computes
Git blob IDs from an offline upstream checkout and fails on drift.

## Runtime DTO authority and documented schema divergence

At this pin, the published OpenAPI describes a liquidity interaction `id` as a
JSON number, while the runtime Rust `solvers-dto` used by the driver declares it
as `String`. The runtime DTO also serializes `internalize` as a required boolean.
The engine therefore preserves the auction's opaque liquidity ID as a JSON
string and always emits `internalize`, including `false`.

This is deliberate and fixture-tested. Moving either source pin requires a
review of the divergence rather than silently following one file.

## Contract rules

The engine consumes the complete pinned auction union, even for liquidity kinds
it does not route, so normalization cannot silently lose fields. It routes only
constant-product, concentrated-liquidity and stable pools. Weighted pools and
foreign limit orders remain known-but-unsupported and are skipped.

Orders with fee policies, wrappers, hooks, flashloan hints or unsupported
balance modes are skipped. An unknown top-level, token, order, fee-policy,
wrapper or known-liquidity field is rejected until reviewed. Notification
payloads remain intentionally extensible and preserve unknown metadata as raw
JSON. Solution IDs in notifications use `json.Number` so values above 2^53 do
not lose precision.

## Updating the pin

An upstream update requires a dedicated pull request that:

1. records the new exact commit and all authoritative blob SHAs;
2. reviews OpenAPI, runtime DTO and driver-construction diffs;
3. updates canonical fixtures and their SHA-256 manifest;
4. regenerates and reviews the language-neutral arithmetic vectors;
5. revalidates stable arithmetic when the math source changes;
6. runs exact-head and pull-request merge-ref CI;
7. documents every new or newly unsupported semantic field before landing.

Do not move this pin as part of an unrelated solver optimization.
'''
write_text("UPSTREAM.md", upstream_md)

roadmap_path = ROOT / "ROADMAP.md"
roadmap = roadmap_path.read_text(encoding="utf-8")
roadmap = roadmap.replace(
    "Status: implementation complete on Draft PR #2; final exact-head and merge-ref\nverification remain before human landing approval.",
    "Status: accepted and squash-merged at `27d0800324358c39f36f240b0fbd5920faf5ee67`.",
)
roadmap = roadmap.replace(
    "## Phase 1 — Continuously verify the CoW wire contract\n\nFoundation already delivered in Phase 0:",
    "## Phase 1 — Continuously verify the CoW wire contract\n\nStatus: implementation in progress from the accepted Phase 0 landing.\n\nFoundation already delivered in Phase 0:",
)
roadmap_path.write_text(roadmap, encoding="utf-8")

phase1_doc = r'''
# Phase 1 — Pinned CoW Wire-Contract Verification

Phase 1 turns the upstream pin into executable, offline evidence.

## Canonical fixtures

`contract/cow-v1/` contains canonical JSON for a representative auction,
solution, notification and language-neutral arithmetic vectors. The manifest
binds each file by SHA-256 and binds the upstream repository, commit and five
authoritative Git blob IDs.

The auction exercises every pinned order and liquidity field, including known
unsupported fee, wrapper, flashloan, weighted-pool and foreign-limit-order
shapes. Only the supported order is solved; unsupported execution semantics are
counted and skipped.

## Fail-closed semantic drift

The solve endpoint validates allowed and required fields before unmarshalling.
Unknown fields in the auction, token, order, nested fee/wrapper structures or
any pinned liquidity variant fail closed. A newly introduced liquidity kind also
fails until its semantics are reviewed. Notifications are the exception because
the upstream contract explicitly makes them extensible; their extra fields are
preserved byte-for-byte as raw JSON values.

## Runtime wire correction

The pinned OpenAPI and runtime Rust DTO disagree about liquidity interaction
IDs. The runtime driver DTO is the executable authority: IDs are opaque strings
and `internalize` is a serialized bool. Fixtures and end-to-end tests enforce
that exact shape.

## Arithmetic vectors

`scripts/generate_reference_vectors.py` independently generates exact integer
vectors in Python for constant-product, concentrated-liquidity, stable-pool and
TickMath behavior. Go tests consume the language-neutral JSON and require exact
outputs. CI also runs the generator in `--check` mode to prevent stale vectors.

## Verification commands

```sh
go run ./cmd/contractcheck -root .
python3 scripts/generate_reference_vectors.py --check
bash scripts/ci.sh
```

To verify an offline checkout of the pinned upstream source:

```sh
go run ./cmd/contractcheck -root . -upstream-dir /path/to/cowprotocol-services
```

No command fetches the network, signs, submits or touches capital.
'''
write_text("docs/PHASE1_WIRE_CONTRACT.md", phase1_doc)

makefile_path = ROOT / "Makefile"
makefile = makefile_path.read_text(encoding="utf-8")
makefile = makefile.replace(".PHONY: build test race lint ci hooks run report clean", ".PHONY: build test race lint contract ci hooks run report clean")
makefile = makefile.replace(
    '\tgo build -trimpath -ldflags="-s -w" -o bin/report ./cmd/report\n',
    '\tgo build -trimpath -ldflags="-s -w" -o bin/report ./cmd/report\n\tgo build -trimpath -ldflags="-s -w" -o bin/contractcheck ./cmd/contractcheck\n',
)
makefile = makefile.replace("# Every gate, exactly as CI runs them.\nci:", "contract:\n\tgo run ./cmd/contractcheck -root .\n\tpython3 scripts/generate_reference_vectors.py --check\n\n# Every gate, exactly as CI runs them.\nci:")
makefile_path.write_text(makefile, encoding="utf-8")

ci_path = ROOT / "scripts/ci.sh"
ci = ci_path.read_text(encoding="utf-8")
ci = ci.replace(
    'section "Static analysis"\nrun_gate "go vet" go vet ./...\n',
    'section "Pinned wire contract"\nrun_gate "contract fixtures and source pins" go run ./cmd/contractcheck -root .\nrun_gate "cross-language reference vectors" python3 scripts/generate_reference_vectors.py --check\n\nsection "Static analysis"\nrun_gate "go vet" go vet ./...\n',
)
ci = ci.replace(
    'run_gate "solver and report build" go build -trimpath ./cmd/solver ./cmd/report',
    'run_gate "solver, report, and contractcheck build" go build -trimpath ./cmd/solver ./cmd/report ./cmd/contractcheck',
)
ci_path.write_text(ci, encoding="utf-8")

# Ensure fixture IDs and test expectations were all updated.
for path in [
    "internal/api/types.go",
    "internal/server/server.go",
    "internal/server/server_test.go",
    "internal/server/recovery_test.go",
    "internal/server/adversarial_test.go",
]:
    text = (ROOT / path).read_text(encoding="utf-8")
    if "numeric liquidity id" in text or "ParseUint(i.ID" in text:
        raise SystemExit(f"stale numeric-ID contract in {path}")

print("Phase 1 wire-contract implementation written")

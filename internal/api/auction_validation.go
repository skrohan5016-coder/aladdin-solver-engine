package api

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"
)

type auctionWire Auction

// UnmarshalJSON enforces the scalar bounds of the pinned Rust runtime DTO.
// Object shape and unknown settlement-semantic fields are checked separately
// by internal/contract before the server accepts an auction.
func (auction *Auction) UnmarshalJSON(data []byte) error {
	var decoded auctionWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.ID != nil {
		if _, err := strconv.ParseInt(*decoded.ID, 10, 64); err != nil {
			return fmt.Errorf("decode auction id: %w", err)
		}
	}
	if !validDecimalU256(decoded.EffectiveGasPrice, true) {
		return fmt.Errorf("decode auction effectiveGasPrice: expected decimal uint256")
	}
	if _, err := time.Parse(time.RFC3339, decoded.Deadline); err != nil {
		return fmt.Errorf("decode auction deadline: %w", err)
	}
	for address, token := range decoded.Tokens {
		if token.Decimals != nil && (*token.Decimals < 0 || *token.Decimals > math.MaxUint8) {
			return fmt.Errorf("decode token %q decimals: expected uint8", address)
		}
		if !validDecimalU256(token.AvailableBalance, true) {
			return fmt.Errorf("decode token %q availableBalance: expected decimal uint256", address)
		}
		if token.ReferencePrice != "" && !validDecimalU256(token.ReferencePrice, true) {
			return fmt.Errorf("decode token %q referencePrice: expected decimal uint256", address)
		}
	}
	for index, order := range decoded.Orders {
		for name, value := range map[string]string{
			"sellAmount":     order.SellAmount,
			"fullSellAmount": order.FullSellAmount,
			"buyAmount":      order.BuyAmount,
			"fullBuyAmount":  order.FullBuyAmount,
		} {
			if !validDecimalU256(value, false) {
				return fmt.Errorf("decode order %d %s: expected positive decimal uint256", index, name)
			}
		}
		if order.ValidTo < 0 || order.ValidTo > math.MaxUint32 {
			return fmt.Errorf("decode order %d validTo: expected uint32", index)
		}
	}
	*auction = Auction(decoded)
	return nil
}

func validDecimalU256(raw string, allowZero bool) bool {
	if raw == "" || len(raw) > 78 {
		return false
	}
	for index := 0; index < len(raw); index++ {
		if raw[index] < '0' || raw[index] > '9' {
			return false
		}
	}
	value, ok := new(big.Int).SetString(raw, 10)
	if !ok || value.Sign() < 0 || value.BitLen() > 256 {
		return false
	}
	return allowZero || value.Sign() > 0
}

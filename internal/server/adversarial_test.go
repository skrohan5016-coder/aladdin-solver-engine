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
		UID:               uid,
		SellToken:         "0x0000000000000000000000000000000000000001",
		BuyToken:          "0x0000000000000000000000000000000000000002",
		SellAmount:        "1",
		BuyAmount:         "1",
		FullBuyAmount:     "1",
		Kind:              "sell",
		Owner:             "0x0000000000000000000000000000000000000003",
		SellTokenSource:   "erc20",
		BuyTokenDest:      "erc20",
		PreInteractions:   []json.RawMessage{},
		PostInteractions:  []json.RawMessage{},
		PartiallyFillable: false,
	}
}

func TestOpaqueIDAllowedForUnsupportedLiquidity(t *testing.T) {
	auction := validContractAuction()
	auction.Liquidity = []api.Liquidity{{Kind: "weightedProduct", ID: "opaque-pool-id"}}
	if err := validateAuction(auction); err != nil {
		t.Fatalf("unsupported opaque liquidity should be skipped, got %v", err)
	}
}

func TestLiquidityIDsAreOpaqueButUnique(t *testing.T) {
	auction := validContractAuction()
	auction.Liquidity = []api.Liquidity{{Kind: "constantProduct", ID: "cp/opaque/01"}}
	if err := validateAuction(auction); err != nil {
		t.Fatalf("opaque routable ID was rejected: %v", err)
	}

	auction.Liquidity = append(auction.Liquidity, api.Liquidity{Kind: "stable", ID: "cp/opaque/01"})
	if err := validateAuction(auction); err == nil {
		t.Fatal("duplicate opaque liquidity IDs were accepted")
	}
}

func TestSemanticDuplicatesRejected(t *testing.T) {
	t.Run("token address", func(t *testing.T) {
		auction := validContractAuction()
		auction.Tokens = map[string]api.TokenInfo{
			"0xAbC": {AvailableBalance: "0"},
			"0xabc": {AvailableBalance: "0"},
		}
		if err := validateAuction(auction); err == nil {
			t.Fatal("case-insensitive duplicate token addresses were accepted")
		}
	})

	t.Run("order uid", func(t *testing.T) {
		auction := validContractAuction()
		auction.Orders = []api.Order{
			validContractOrder("0xAbC"),
			validContractOrder("0xabc"),
		}
		if err := validateAuction(auction); err == nil {
			t.Fatal("case-insensitive duplicate order UIDs were accepted")
		}
	})
}

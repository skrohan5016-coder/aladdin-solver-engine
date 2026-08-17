package solve

import (
	"encoding/json"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/api"
)

func TestSettlementSemanticOrderFieldsFailClosed(t *testing.T) {
	base := api.Order{
		UID: "0x01", SellToken: tokA, BuyToken: tokB,
		SellAmount: "100", FullSellAmount: "100", BuyAmount: "90", FullBuyAmount: "90",
		Kind: "sell", SellTokenSource: "erc20", BuyTokenDest: "erc20",
	}
	cases := map[string]func(*api.Order){
		"fee policy": func(order *api.Order) { order.FeePolicies = []api.FeePolicy{{Kind: "volume"}} },
		"wrapper": func(order *api.Order) {
			order.Wrappers = []json.RawMessage{json.RawMessage(`{"address":"0x1","data":"0x"}`)}
		},
		"flashloan hint":  func(order *api.Order) { order.FlashloanHint = json.RawMessage(`{"amount":"1"}`) },
		"pre interaction": func(order *api.Order) { order.PreInteractions = []json.RawMessage{json.RawMessage(`{}`)} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			order := base
			mutate(&order)
			eligibleOrders, unsupported := eligible([]api.Order{order}, 10)
			if len(eligibleOrders) != 0 || unsupported != 1 {
				t.Fatalf("semantic field was ignored: eligible=%d unsupported=%d", len(eligibleOrders), unsupported)
			}
		})
	}
}

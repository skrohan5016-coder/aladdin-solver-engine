// Package api contains the wire types for the CoW Protocol solver-engine
// interface, as defined by cowprotocol/services crates/solvers/openapi.yml.
//
// All token amounts are canonical base-10 uint256 strings. They are never
// parsed into floats anywhere in this codebase.
package api

import (
	"encoding/json"
	"strconv"
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
	FullSellAmount    string            `json:"fullSellAmount,omitempty"`
	BuyAmount         string            `json:"buyAmount"`
	FullBuyAmount     string            `json:"fullBuyAmount"`
	FeePolicies       []FeePolicy       `json:"feePolicies,omitempty"`
	ValidTo           int64             `json:"validTo"`
	Kind              string            `json:"kind"` // "sell" | "buy"
	Receiver          string            `json:"receiver,omitempty"`
	Owner             string            `json:"owner"`
	PartiallyFillable bool              `json:"partiallyFillable"`
	PreInteractions   []json.RawMessage `json:"preInteractions"`
	PostInteractions  []json.RawMessage `json:"postInteractions"`
	SellTokenSource   string            `json:"sellTokenSource"`
	BuyTokenDest      string            `json:"buyTokenDestination"`
	Class             string            `json:"class"` // "market" | "limit"
	AppData           string            `json:"appData"`
	FlashloanHint     json.RawMessage   `json:"flashloanHint,omitempty"`
	SigningScheme     string            `json:"signingScheme"`
	Signature         string            `json:"signature"`
}

type FeePolicy struct {
	Kind            string   `json:"kind"`
	Factor          *float64 `json:"factor,omitempty"`
	MaxVolumeFactor *float64 `json:"maxVolumeFactor,omitempty"`
}

// Liquidity is the flattened union of every pool kind the driver may send.
// Unknown kinds are preserved but ignored by the router.
type Liquidity struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Address     string `json:"address"`
	GasEstimate string `json:"gasEstimate"`

	// constantProduct / weightedProduct / stable
	Tokens json.RawMessage `json:"tokens"`
	Fee    string          `json:"fee"`
	Router string          `json:"router,omitempty"`

	// stable
	AmplificationParameter string `json:"amplificationParameter,omitempty"`

	// concentratedLiquidity
	SqrtPrice    string            `json:"sqrtPrice,omitempty"`
	Liquidity    string            `json:"liquidity,omitempty"`
	Tick         *int32            `json:"tick,omitempty"`
	LiquidityNet map[string]string `json:"liquidityNet,omitempty"`
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
	Kind           string `json:"kind"` // "fulfillment"
	Order          string `json:"order"`
	ExecutedAmount string `json:"executedAmount"`
	Fee            string `json:"fee,omitempty"`
}

// Interaction is a liquidity interaction. The driver expects `id` as a JSON
// number here even though the auction carries it as a string, so it is
// re-encoded on the way out.
type Interaction struct {
	Kind         string `json:"kind"` // "liquidity"
	ID           string `json:"-"`
	InputToken   string `json:"inputToken"`
	OutputToken  string `json:"outputToken"`
	InputAmount  string `json:"inputAmount"`
	OutputAmount string `json:"outputAmount"`
	Internalize  bool   `json:"internalize,omitempty"`
}

func (i Interaction) MarshalJSON() ([]byte, error) {
	type alias Interaction
	out := struct {
		ID json.RawMessage `json:"id"`
		alias
	}{alias: alias(i)}
	if n, err := strconv.ParseUint(i.ID, 10, 64); err == nil {
		out.ID = json.RawMessage(strconv.FormatUint(n, 10))
	} else {
		b, _ := json.Marshal(i.ID)
		out.ID = b
	}
	return json.Marshal(out)
}

// ---------- Notification (driver -> solver) ----------

type Notification struct {
	AuctionID  string  `json:"auctionId"`
	SolutionID float64 `json:"solutionId"`
	Kind       string  `json:"kind"`
}

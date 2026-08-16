// Package api contains the wire types for the CoW Protocol solver-engine
// interface, as defined by the pinned cowprotocol/services contract.
//
// All token amounts are canonical base-10 uint256 strings. They are never
// parsed into floats anywhere in this codebase.
package api

import "encoding/json"

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
	Wrappers          []json.RawMessage `json:"wrappers,omitempty"`
	SigningScheme     string            `json:"signingScheme"`
	Signature         string            `json:"signature"`
}

type FeePolicy struct {
	Kind            string    `json:"kind"`
	Factor          *float64  `json:"factor,omitempty"`
	MaxVolumeFactor *float64  `json:"maxVolumeFactor,omitempty"`
	Quote           *FeeQuote `json:"quote,omitempty"`
}

type FeeQuote struct {
	SellAmount string `json:"sellAmount"`
	BuyAmount  string `json:"buyAmount"`
	Fee        string `json:"fee"`
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

	// Balancer stable / weighted product
	AmplificationParameter string `json:"amplificationParameter,omitempty"`
	BalancerPoolID         string `json:"balancerPoolId,omitempty"`
	Version                string `json:"version,omitempty"`

	// concentratedLiquidity
	SqrtPrice    string            `json:"sqrtPrice,omitempty"`
	Liquidity    string            `json:"liquidity,omitempty"`
	Tick         *int32            `json:"tick,omitempty"`
	LiquidityNet map[string]string `json:"liquidityNet,omitempty"`

	// foreign limit order
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
	Kind           string `json:"kind"` // "fulfillment"
	Order          string `json:"order"`
	ExecutedAmount string `json:"executedAmount"`
	Fee            string `json:"fee,omitempty"`
}

// Interaction mirrors the runtime Rust solution DTO. Liquidity IDs remain
// opaque strings, and internalize is always emitted explicitly.
type Interaction struct {
	Kind         string `json:"kind"` // "liquidity"
	ID           string `json:"id"`
	InputToken   string `json:"inputToken"`
	OutputToken  string `json:"outputToken"`
	InputAmount  string `json:"inputAmount"`
	OutputAmount string `json:"outputAmount"`
	Internalize  bool   `json:"internalize"`
}

// ---------- Notification (driver -> solver) ----------

// Notification keeps all unknown metadata because notification payloads are
// explicitly extensible and are the evidence needed to explain shadow results.
type Notification struct {
	AuctionID  string                     `json:"auctionId"`
	SolutionID float64                    `json:"solutionId"`
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
		if err := json.Unmarshal(raw, &n.SolutionID); err != nil {
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
	if err := put("solutionId", n.SolutionID); err != nil {
		return nil, err
	}
	if err := put("kind", n.Kind); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// Package api contains the wire types for the CoW Protocol solver-engine
// interface, as defined by the pinned cowprotocol/services contract.
//
// All token amounts are canonical base-10 uint256 strings. They are never
// parsed into floats anywhere in this codebase.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
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

type Liquidity struct {
	Kind                   string            `json:"kind"`
	ID                     string            `json:"id"`
	Address                string            `json:"address"`
	GasEstimate            string            `json:"gasEstimate"`
	Tokens                 json.RawMessage   `json:"tokens"`
	Fee                    string            `json:"fee"`
	Router                 string            `json:"router,omitempty"`
	AmplificationParameter string            `json:"amplificationParameter,omitempty"`
	BalancerPoolID         string            `json:"balancerPoolId,omitempty"`
	Version                string            `json:"version,omitempty"`
	SqrtPrice              string            `json:"sqrtPrice,omitempty"`
	Liquidity              string            `json:"liquidity,omitempty"`
	Tick                   *int32            `json:"tick,omitempty"`
	LiquidityNet           map[string]string `json:"liquidityNet,omitempty"`
	Hash                   string            `json:"hash,omitempty"`
	MakerToken             string            `json:"makerToken,omitempty"`
	TakerToken             string            `json:"takerToken,omitempty"`
	MakerAmount            string            `json:"makerAmount,omitempty"`
	TakerAmount            string            `json:"takerAmount,omitempty"`
	TakerTokenFeeAmount    string            `json:"takerTokenFeeAmount,omitempty"`
}

type TokenReserve struct {
	Balance string `json:"balance"`
}

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

type Interaction struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	InputToken   string `json:"inputToken"`
	OutputToken  string `json:"outputToken"`
	InputAmount  string `json:"inputAmount"`
	OutputAmount string `json:"outputAmount"`
	Internalize  bool   `json:"internalize"`
}

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
	raw, err := takeRequiredNotificationField(fields, "auctionId")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &n.AuctionID); err != nil {
		return err
	}
	raw, err = takeRequiredNotificationField(fields, "solutionId")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &n.SolutionID); err != nil {
		return err
	}
	raw, err = takeRequiredNotificationField(fields, "kind")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &n.Kind); err != nil {
		return err
	}
	n.Extra = fields
	return nil
}

func takeRequiredNotificationField(fields map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("notification: missing or null required field %q", name)
	}
	delete(fields, name)
	return raw, nil
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

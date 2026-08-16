// Package api contains the wire types for the CoW Protocol solver-engine
// interface, as defined by the pinned cowprotocol/services contract.
//
// All token amounts are canonical base-10 uint256 strings. They are never
// parsed into floats anywhere in this codebase.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// ---------- Notification (driver -> solver) ----------

// Notification follows the accepted runtime DTO. AuctionID and SolutionID are
// optional; SolutionID is either one u64 or a merged u64 array.
type Notification struct {
	AuctionID  *string                    `json:"auctionId"`
	SolutionID *NotificationSolutionID    `json:"solutionId"`
	Kind       string                     `json:"kind"`
	Extra      map[string]json.RawMessage `json:"-"`
}

type NotificationSolutionID struct {
	single *uint64
	merged []uint64
}

func NewSingleNotificationSolutionID(value uint64) *NotificationSolutionID {
	return &NotificationSolutionID{single: &value}
}

func NewMergedNotificationSolutionID(values ...uint64) *NotificationSolutionID {
	copied := append([]uint64(nil), values...)
	if copied == nil {
		copied = []uint64{}
	}
	return &NotificationSolutionID{merged: copied}
}

func (id *NotificationSolutionID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return errors.New("notification solution id receiver is nil")
	}
	*id = NotificationSolutionID{}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return errors.New("notification solution id must be uint64 or uint64 array")
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return fmt.Errorf("decode merged notification solution ids: %w", err)
		}
		values := make([]uint64, len(items))
		for index, item := range items {
			if bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
				return fmt.Errorf("decode merged notification solution id %d: null is not uint64", index)
			}
			if err := json.Unmarshal(item, &values[index]); err != nil {
				return fmt.Errorf("decode merged notification solution id %d: %w", index, err)
			}
		}
		if items == nil {
			values = []uint64{}
		}
		id.merged = values
		return nil
	}
	var value uint64
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return fmt.Errorf("decode notification solution id: %w", err)
	}
	id.single = &value
	return nil
}

func (id NotificationSolutionID) MarshalJSON() ([]byte, error) {
	switch {
	case id.single != nil && id.merged == nil:
		return json.Marshal(*id.single)
	case id.single == nil && id.merged != nil:
		return json.Marshal(id.merged)
	default:
		return nil, errors.New("notification solution id has an invalid representation")
	}
}

func (id *NotificationSolutionID) String() string {
	if id == nil {
		return ""
	}
	data, err := json.Marshal(id)
	if err != nil {
		return ""
	}
	return string(data)
}

func (n Notification) AuctionIDString() string {
	if n.AuctionID == nil {
		return ""
	}
	return *n.AuctionID
}

func (n Notification) SolutionIDString() string {
	return n.SolutionID.String()
}

func (n *Notification) UnmarshalJSON(data []byte) error {
	*n = Notification{}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if raw, ok := fields["auctionId"]; ok {
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("decode notification auctionId: %w", err)
			}
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				return fmt.Errorf("decode notification auctionId: %w", err)
			}
			n.AuctionID = &value
		}
		delete(fields, "auctionId")
	}
	if raw, ok := fields["solutionId"]; ok {
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var value NotificationSolutionID
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			n.SolutionID = &value
		}
		delete(fields, "solutionId")
	}
	rawKind, ok := fields["kind"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawKind), []byte("null")) {
		return errors.New("notification: missing or null required field \"kind\"")
	}
	if err := json.Unmarshal(rawKind, &n.Kind); err != nil {
		return err
	}
	if n.Kind == "" {
		return errors.New("notification kind is empty")
	}
	delete(fields, "kind")
	n.Extra = fields
	return nil
}

func (n Notification) MarshalJSON() ([]byte, error) {
	if n.Kind == "" {
		return nil, errors.New("notification kind is empty")
	}
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

// Package contract validates the pinned CoW solver-engine wire contract.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

const (
	UpstreamRepository = "cowprotocol/services"
	UpstreamCommit     = "20b3a62f222ad278502fb7e85cae4938e7f26f65"
)

var UpstreamFiles = map[string]string{
	"crates/driver/src/infra/solver/dto/auction.rs":                "f857f86838ce8a2a0b9ab0c7185e23eb4c8bcb9f",
	"crates/liquidity-sources/src/balancer_v2/swap/stable_math.rs": "3d181998518804abe621f739c033f0e0d75d9dd1",
	"crates/solvers-dto/src/auction.rs":                            "6c82fd4e461a32d73453feb68d79686642f802d6",
	"crates/solvers-dto/src/notification.rs":                       "dbbff28c235d3ba7fc559d774ba06a305385fffb",
	"crates/solvers-dto/src/solution.rs":                           "816486e47ba0ac8d19da8a31ee722c103ee6c416",
	"crates/solvers/openapi.yml":                                   "64a2466292446ea5f637c809f754fb4a31211a16",
}

// NormalizeJSON produces a deterministic JSON representation while preserving
// integer lexemes through json.Number. Duplicate keys and trailing values are
// rejected before normalization.
func NormalizeJSON(data []byte) ([]byte, error) {
	if err := ValidateUniqueJSON(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(normalized, '\n'), nil
}

// ValidateUniqueJSON rejects duplicate keys at any depth and trailing values.
func ValidateUniqueJSON(data []byte) error {
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

func ValidateAuctionJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	root, err := object(data, "auction")
	if err != nil {
		return err
	}
	if err := fields(root, "auction",
		[]string{"tokens", "orders", "liquidity", "effectiveGasPrice", "deadline", "surplusCapturingJitOrderOwners"},
		[]string{"id", "tokens", "orders", "liquidity", "effectiveGasPrice", "deadline", "surplusCapturingJitOrderOwners"}); err != nil {
		return err
	}
	if raw, ok := root["id"]; ok && !isNull(raw) {
		if err := stringValue(raw, "auction.id"); err != nil {
			return err
		}
	}
	if err := stringValue(root["effectiveGasPrice"], "auction.effectiveGasPrice"); err != nil {
		return err
	}
	if err := stringValue(root["deadline"], "auction.deadline"); err != nil {
		return err
	}
	if _, err := array(root["surplusCapturingJitOrderOwners"], "auction.surplusCapturingJitOrderOwners"); err != nil {
		return err
	}

	tokens, err := object(root["tokens"], "auction.tokens")
	if err != nil {
		return err
	}
	for address, raw := range tokens {
		info, err := object(raw, "auction.tokens["+address+"]")
		if err != nil {
			return err
		}
		if err := fields(info, "auction.tokens["+address+"]", []string{"availableBalance", "trusted"}, []string{"decimals", "symbol", "referencePrice", "availableBalance", "trusted"}); err != nil {
			return err
		}
		if err := stringValue(info["availableBalance"], "token.availableBalance"); err != nil {
			return err
		}
		if err := boolValue(info["trusted"], "token.trusted"); err != nil {
			return err
		}
	}

	orders, err := array(root["orders"], "auction.orders")
	if err != nil {
		return err
	}
	for index, raw := range orders {
		if err := validateOrder(raw, fmt.Sprintf("auction.orders[%d]", index)); err != nil {
			return err
		}
	}
	liquidity, err := array(root["liquidity"], "auction.liquidity")
	if err != nil {
		return err
	}
	for index, raw := range liquidity {
		if err := validateLiquidity(raw, fmt.Sprintf("auction.liquidity[%d]", index)); err != nil {
			return err
		}
	}
	return nil
}

func validateOrder(raw json.RawMessage, path string) error {
	value, err := object(raw, path)
	if err != nil {
		return err
	}
	required := []string{"uid", "sellToken", "buyToken", "sellAmount", "fullSellAmount", "buyAmount", "fullBuyAmount", "validTo", "kind", "owner", "partiallyFillable", "preInteractions", "postInteractions", "sellTokenSource", "buyTokenDestination", "class", "appData", "signingScheme", "signature"}
	allowed := append(append([]string{}, required...), "feePolicies", "receiver", "flashloanHint", "wrappers")
	if err := fields(value, path, required, allowed); err != nil {
		return err
	}
	for _, name := range []string{"uid", "sellToken", "buyToken", "sellAmount", "fullSellAmount", "buyAmount", "fullBuyAmount", "kind", "owner", "sellTokenSource", "buyTokenDestination", "class", "appData", "signingScheme", "signature"} {
		if err := stringValue(value[name], path+"."+name); err != nil {
			return err
		}
	}
	if err := numberValue(value["validTo"], path+".validTo"); err != nil {
		return err
	}
	if err := boolValue(value["partiallyFillable"], path+".partiallyFillable"); err != nil {
		return err
	}
	for _, name := range []string{"preInteractions", "postInteractions"} {
		items, err := array(value[name], path+"."+name)
		if err != nil {
			return err
		}
		for i, item := range items {
			interaction, err := object(item, fmt.Sprintf("%s.%s[%d]", path, name, i))
			if err != nil {
				return err
			}
			if err := fields(interaction, path+"."+name, []string{"target", "value", "callData"}, []string{"target", "value", "callData"}); err != nil {
				return err
			}
		}
	}
	if raw, ok := value["feePolicies"]; ok {
		items, err := array(raw, path+".feePolicies")
		if err != nil {
			return err
		}
		for i, item := range items {
			if err := validateFeePolicy(item, fmt.Sprintf("%s.feePolicies[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	if raw, ok := value["flashloanHint"]; ok && !isNull(raw) {
		hint, err := object(raw, path+".flashloanHint")
		if err != nil {
			return err
		}
		if err := fields(hint, path+".flashloanHint", []string{"liquidityProvider", "protocolAdapter", "receiver", "token", "amount"}, []string{"liquidityProvider", "protocolAdapter", "receiver", "token", "amount"}); err != nil {
			return err
		}
	}
	if raw, ok := value["wrappers"]; ok {
		items, err := array(raw, path+".wrappers")
		if err != nil {
			return err
		}
		for i, item := range items {
			wrapper, err := object(item, fmt.Sprintf("%s.wrappers[%d]", path, i))
			if err != nil {
				return err
			}
			if err := fields(wrapper, path+".wrappers", []string{"address", "data"}, []string{"address", "data", "isOmittable"}); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFeePolicy(raw json.RawMessage, path string) error {
	value, err := object(raw, path)
	if err != nil {
		return err
	}
	kind, err := requiredString(value, "kind", path)
	if err != nil {
		return err
	}
	switch kind {
	case "surplus":
		return fields(value, path, []string{"kind", "factor", "maxVolumeFactor"}, []string{"kind", "factor", "maxVolumeFactor"})
	case "priceImprovement":
		if err := fields(value, path, []string{"kind", "factor", "maxVolumeFactor", "quote"}, []string{"kind", "factor", "maxVolumeFactor", "quote"}); err != nil {
			return err
		}
		quote, err := object(value["quote"], path+".quote")
		if err != nil {
			return err
		}
		return fields(quote, path+".quote", []string{"sellAmount", "buyAmount", "fee"}, []string{"sellAmount", "buyAmount", "fee"})
	case "volume":
		return fields(value, path, []string{"kind", "factor"}, []string{"kind", "factor"})
	default:
		return fmt.Errorf("%s.kind: unsupported fee policy %q", path, kind)
	}
}

func validateLiquidity(raw json.RawMessage, path string) error {
	value, err := object(raw, path)
	if err != nil {
		return err
	}
	kind, err := requiredString(value, "kind", path)
	if err != nil {
		return err
	}
	common := []string{"kind", "id", "address", "gasEstimate"}
	var required, allowed []string
	switch kind {
	case "constantProduct":
		required = append(common, "tokens", "fee", "router")
		allowed = required
	case "stable":
		required = append(common, "tokens", "amplificationParameter", "fee", "balancerPoolId")
		allowed = required
	case "weightedProduct":
		required = append(common, "tokens", "fee", "version", "balancerPoolId")
		allowed = required
	case "concentratedLiquidity":
		required = append(common, "tokens", "sqrtPrice", "liquidity", "tick", "liquidityNet", "fee", "router")
		allowed = required
	case "limitOrder":
		required = append(common, "hash", "makerToken", "takerToken", "makerAmount", "takerAmount", "takerTokenFeeAmount")
		allowed = required
	default:
		return fmt.Errorf("%s.kind: unsupported upstream liquidity kind %q", path, kind)
	}
	if err := fields(value, path, required, allowed); err != nil {
		return err
	}
	for _, name := range []string{"id", "address", "gasEstimate"} {
		if err := stringValue(value[name], path+"."+name); err != nil {
			return err
		}
	}
	if kind == "constantProduct" || kind == "stable" || kind == "weightedProduct" {
		reserves, err := object(value["tokens"], path+".tokens")
		if err != nil {
			return err
		}
		for token, reserveRaw := range reserves {
			reserve, err := object(reserveRaw, path+".tokens["+token+"]")
			if err != nil {
				return err
			}
			requiredReserve := []string{"balance"}
			allowedReserve := []string{"balance"}
			if kind == "stable" {
				requiredReserve = append(requiredReserve, "scalingFactor")
				allowedReserve = requiredReserve
			}
			if kind == "weightedProduct" {
				requiredReserve = append(requiredReserve, "scalingFactor", "weight")
				allowedReserve = requiredReserve
			}
			if err := fields(reserve, path+".tokens["+token+"]", requiredReserve, allowedReserve); err != nil {
				return err
			}
		}
	}
	if kind == "concentratedLiquidity" {
		if _, err := array(value["tokens"], path+".tokens"); err != nil {
			return err
		}
		if _, err := object(value["liquidityNet"], path+".liquidityNet"); err != nil {
			return err
		}
	}
	return nil
}

func ValidateSolveResponseJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	root, err := object(data, "response")
	if err != nil {
		return err
	}
	if err := fields(root, "response", []string{"solutions"}, []string{"solutions"}); err != nil {
		return err
	}
	solutions, err := array(root["solutions"], "response.solutions")
	if err != nil {
		return err
	}
	for i, raw := range solutions {
		path := fmt.Sprintf("response.solutions[%d]", i)
		solution, err := object(raw, path)
		if err != nil {
			return err
		}
		if err := fields(solution, path, []string{"id", "prices", "trades", "interactions"}, []string{"id", "prices", "trades", "preInteractions", "interactions", "postInteractions", "gas", "maxFeePerGas", "maxPriorityFeePerGas", "flashloans", "wrappers"}); err != nil {
			return err
		}
		if err := numberValue(solution["id"], path+".id"); err != nil {
			return err
		}
		prices, err := object(solution["prices"], path+".prices")
		if err != nil {
			return err
		}
		for token, price := range prices {
			if err := stringValue(price, path+".prices["+token+"]"); err != nil {
				return err
			}
		}
		trades, err := array(solution["trades"], path+".trades")
		if err != nil {
			return err
		}
		for j, tradeRaw := range trades {
			tradePath := fmt.Sprintf("%s.trades[%d]", path, j)
			trade, err := object(tradeRaw, tradePath)
			if err != nil {
				return err
			}
			if err := fields(trade, tradePath, []string{"kind", "order", "executedAmount"}, []string{"kind", "order", "executedAmount", "fee"}); err != nil {
				return err
			}
			kind, err := requiredString(trade, "kind", tradePath)
			if err != nil || kind != "fulfillment" {
				return fmt.Errorf("%s.kind: expected fulfillment", tradePath)
			}
		}
		interactions, err := array(solution["interactions"], path+".interactions")
		if err != nil {
			return err
		}
		for j, interactionRaw := range interactions {
			interactionPath := fmt.Sprintf("%s.interactions[%d]", path, j)
			interaction, err := object(interactionRaw, interactionPath)
			if err != nil {
				return err
			}
			if err := fields(interaction, interactionPath, []string{"kind", "id", "inputToken", "outputToken", "inputAmount", "outputAmount", "internalize"}, []string{"kind", "id", "inputToken", "outputToken", "inputAmount", "outputAmount", "internalize"}); err != nil {
				return err
			}
			kind, err := requiredString(interaction, "kind", interactionPath)
			if err != nil || kind != "liquidity" {
				return fmt.Errorf("%s.kind: expected liquidity", interactionPath)
			}
			if err := stringValue(interaction["id"], interactionPath+".id"); err != nil {
				return err
			}
			if err := boolValue(interaction["internalize"], interactionPath+".internalize"); err != nil {
				return err
			}
		}
	}
	return nil
}

func ValidateNotificationJSON(data []byte) error {
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	value, err := object(data, "notification")
	if err != nil {
		return err
	}
	kind, ok := value["kind"]
	if !ok {
		return errors.New("notification: missing required field \"kind\"")
	}
	if err := stringValue(kind, "notification.kind"); err != nil {
		return err
	}
	if raw, ok := value["auctionId"]; ok && !isNull(raw) {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return errors.New("notification.auctionId: expected decimal string or null")
		}
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return fmt.Errorf("notification.auctionId: invalid i64: %w", err)
		}
	}
	if raw, ok := value["solutionId"]; ok && !isNull(raw) {
		if err := notificationSolutionIDValue(raw, "notification.solutionId"); err != nil {
			return err
		}
	}
	return nil
}

func notificationSolutionIDValue(raw json.RawMessage, path string) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		items, err := array(raw, path)
		if err != nil {
			return err
		}
		for index, item := range items {
			if err := uint64NumberValue(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	}
	return uint64NumberValue(raw, path)
}

func uint64NumberValue(raw json.RawMessage, path string) error {
	if isNull(raw) {
		return fmt.Errorf("%s: expected uint64", path)
	}
	var value uint64
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s: expected uint64: %w", path, err)
	}
	return nil
}
func object(raw []byte, path string) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s: expected object: %w", path, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%s: expected object", path)
	}
	return value, nil
}

func array(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	var value []json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s: expected array: %w", path, err)
	}
	if value == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("[]")) {
		return nil, fmt.Errorf("%s: expected array", path)
	}
	return value, nil
}

func fields(value map[string]json.RawMessage, path string, required, allowed []string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	for _, name := range required {
		if _, ok := value[name]; !ok {
			return fmt.Errorf("%s: missing required field %q", path, name)
		}
	}
	var unknown []string
	for name := range value {
		if _, ok := allowedSet[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("%s: unknown settlement-semantic field(s): %v", path, unknown)
	}
	return nil
}

func requiredString(value map[string]json.RawMessage, name, path string) (string, error) {
	raw, ok := value[name]
	if !ok {
		return "", fmt.Errorf("%s: missing required field %q", path, name)
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("%s.%s: expected string", path, name)
	}
	return decoded, nil
}

func stringValue(raw json.RawMessage, path string) error {
	if isNull(raw) {
		return fmt.Errorf("%s: expected string", path)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s: expected string", path)
	}
	return nil
}

func boolValue(raw json.RawMessage, path string) error {
	if isNull(raw) {
		return fmt.Errorf("%s: expected boolean", path)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%s: expected boolean", path)
	}
	return nil
}

func numberValue(raw json.RawMessage, path string) error {
	if isNull(raw) {
		return fmt.Errorf("%s: expected number", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value json.Number
	if err := decoder.Decode(&value); err != nil || value.String() == "" {
		return fmt.Errorf("%s: expected number", path)
	}
	return nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

func validateNotificationKind(kind string, fields map[string]json.RawMessage) error {
	switch kind {
	case "timeout", "emptySolution", "duplicatedSolutionId", "invalidClearingPrices",
		"invalidExecutedAmount", "settlementStarted", "cancelled", "expired", "fail",
		"postprocessingTimedOut":
		return nil
	case "simulationFailed":
		if err := notificationUint64Field(fields, "block"); err != nil {
			return err
		}
		if err := notificationTransactionField(fields, "tx"); err != nil {
			return err
		}
		return notificationBoolField(fields, "succeededOnce")
	case "missingPrice":
		return notificationAddressField(fields, "tokenAddress")
	case "nonBufferableTokensUsed":
		return notificationAddressSetField(fields, "tokens")
	case "solverAccountInsufficientBalance":
		return notificationU256Field(fields, "required")
	case "success", "revert":
		return notificationFixedHexField(fields, "transaction", 32)
	case "driverError", "deserializationError":
		return notificationStringField(fields, "reason")
	default:
		return fmt.Errorf("notification kind %q is not present in the pinned runtime DTO", kind)
	}
}

func notificationRequiredField(fields map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := fields[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("notification kind field %q is missing or null", name)
	}
	return raw, nil
}

func notificationStringField(fields map[string]json.RawMessage, name string) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("notification kind field %q must be a string: %w", name, err)
	}
	return nil
}

func notificationBoolField(fields map[string]json.RawMessage, name string) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("notification kind field %q must be a boolean: %w", name, err)
	}
	return nil
}

func notificationUint64Field(fields map[string]json.RawMessage, name string) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var value uint64
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("notification kind field %q must be uint64: %w", name, err)
	}
	return nil
}

func notificationAddressField(fields map[string]json.RawMessage, name string) error {
	return notificationFixedHexField(fields, name, 20)
}

func notificationFixedHexField(fields map[string]json.RawMessage, name string, size int) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("notification kind field %q must be a hex string: %w", name, err)
	}
	if err := validateHexBytes(value, size); err != nil {
		return fmt.Errorf("notification kind field %q: %w", name, err)
	}
	return nil
}

func notificationAddressSetField(fields map[string]json.RawMessage, name string) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return fmt.Errorf("notification kind field %q must be an address array", name)
	}
	for index, value := range values {
		if err := validateHexBytes(value, 20); err != nil {
			return fmt.Errorf("notification kind field %q[%d]: %w", name, index, err)
		}
	}
	return nil
}

func notificationU256Field(fields map[string]json.RawMessage, name string) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("notification kind field %q must be a hex-or-decimal uint256 string: %w", name, err)
	}
	if !validHexOrDecimalU256(value) {
		return fmt.Errorf("notification kind field %q must be a hex-or-decimal uint256 string", name)
	}
	return nil
}

func notificationTransactionField(fields map[string]json.RawMessage, name string) error {
	raw, err := notificationRequiredField(fields, name)
	if err != nil {
		return err
	}
	var tx map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tx); err != nil || tx == nil {
		return fmt.Errorf("notification kind field %q must be a transaction object", name)
	}
	for _, field := range []string{"from", "to"} {
		if err := notificationAddressField(tx, field); err != nil {
			return fmt.Errorf("notification transaction: %w", err)
		}
	}
	input, err := notificationRequiredField(tx, "input")
	if err != nil {
		return fmt.Errorf("notification transaction: %w", err)
	}
	var inputHex string
	if err := json.Unmarshal(input, &inputHex); err != nil {
		return fmt.Errorf("notification transaction input must be a hex string: %w", err)
	}
	if err := validateHexBytes(inputHex, -1); err != nil {
		return fmt.Errorf("notification transaction input: %w", err)
	}
	if err := notificationU256Field(tx, "value"); err != nil {
		return fmt.Errorf("notification transaction: %w", err)
	}
	accessListRaw, err := notificationRequiredField(tx, "accessList")
	if err != nil {
		return fmt.Errorf("notification transaction: %w", err)
	}
	var accessList []map[string]json.RawMessage
	if err := json.Unmarshal(accessListRaw, &accessList); err != nil || accessList == nil {
		return errors.New("notification transaction accessList must be an array")
	}
	for index, item := range accessList {
		if err := notificationAddressField(item, "address"); err != nil {
			return fmt.Errorf("notification transaction accessList[%d]: %w", index, err)
		}
		keysRaw, err := notificationRequiredField(item, "storageKeys")
		if err != nil {
			return fmt.Errorf("notification transaction accessList[%d]: %w", index, err)
		}
		var keys []string
		if err := json.Unmarshal(keysRaw, &keys); err != nil || keys == nil {
			return fmt.Errorf("notification transaction accessList[%d].storageKeys must be an array", index)
		}
		for keyIndex, key := range keys {
			if err := validateHexBytes(key, 32); err != nil {
				return fmt.Errorf("notification transaction accessList[%d].storageKeys[%d]: %w", index, keyIndex, err)
			}
		}
	}
	return nil
}

func validateHexBytes(value string, size int) error {
	if !strings.HasPrefix(value, "0x") {
		return errors.New("expected 0x-prefixed hex")
	}
	encoded := value[2:]
	if len(encoded)%2 != 0 {
		return errors.New("expected an even number of hex digits")
	}
	if size >= 0 && len(encoded) != size*2 {
		return fmt.Errorf("expected %d bytes", size)
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("invalid hex: %w", err)
	}
	return nil
}

func validHexOrDecimalU256(value string) bool {
	var parsed *big.Int
	var ok bool
	if strings.HasPrefix(value, "0x") {
		if len(value) <= 2 {
			return false
		}
		parsed, ok = new(big.Int).SetString(value[2:], 16)
	} else {
		if value == "" {
			return false
		}
		for index := 0; index < len(value); index++ {
			if value[index] < '0' || value[index] > '9' {
				return false
			}
		}
		parsed, ok = new(big.Int).SetString(value, 10)
	}
	return ok && parsed.Sign() >= 0 && parsed.BitLen() <= 256
}

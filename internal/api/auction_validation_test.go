package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAuctionScalarBoundsMatchPinnedRuntimeDTO(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "contracts", "auction-direct.json")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var auction Auction
	if err := json.Unmarshal(data, &auction); err != nil {
		t.Fatalf("pinned auction fixture was rejected: %v", err)
	}

	var original map[string]any
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any){
		"auction id outside i64": func(value map[string]any) {
			value["id"] = "9223372036854775808"
		},
		"token decimals outside u8": func(value map[string]any) {
			for _, token := range value["tokens"].(map[string]any) {
				token.(map[string]any)["decimals"] = float64(256)
				break
			}
		},
		"effective gas price outside uint256": func(value map[string]any) {
			value["effectiveGasPrice"] = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
		},
		"invalid deadline": func(value map[string]any) {
			value["deadline"] = "not-rfc3339"
		},
		"empty full sell amount": func(value map[string]any) {
			value["orders"].([]any)[0].(map[string]any)["fullSellAmount"] = ""
		},
		"validTo outside u32": func(value map[string]any) {
			value["orders"].([]any)[0].(map[string]any)["validTo"] = float64(4294967296)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var value map[string]any
			if err := json.Unmarshal(encoded, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			encoded, err = json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Auction
			if err := json.Unmarshal(encoded, &decoded); err == nil {
				t.Fatalf("invalid auction scalar was accepted: %s", encoded)
			}
		})
	}
}

package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPinnedFixturesValidate(t *testing.T) {
	for name, validate := range map[string]func([]byte) error{
		"auction-direct.json":        ValidateAuctionJSON,
		"auction-all-liquidity.json": ValidateAuctionJSON,
		"solution-direct.json":       ValidateSolveResponseJSON,
		"notification-extra.json":    ValidateNotificationJSON,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validate(fixture(t, name)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAuctionRejectsUnknownSettlementSemanticField(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal(fixture(t, "auction-direct.json"), &value); err != nil {
		t.Fatal(err)
	}
	orders := value["orders"].([]any)
	orders[0].(map[string]any)["newExecutionAuthority"] = map[string]any{"enabled": true}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAuctionJSON(data); err == nil {
		t.Fatal("unknown order execution field was silently accepted")
	}
}

func TestSolutionRequiresRuntimeDTOInteractionShape(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal(fixture(t, "solution-direct.json"), &value); err != nil {
		t.Fatal(err)
	}
	interaction := value["solutions"].([]any)[0].(map[string]any)["interactions"].([]any)[0].(map[string]any)
	delete(interaction, "internalize")
	interaction["id"] = float64(7)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSolveResponseJSON(data); err == nil {
		t.Fatal("OpenAPI-only numeric ID shape was accepted over the runtime DTO")
	}
}

func TestNormalizationIsOrderIndependent(t *testing.T) {
	left, err := NormalizeJSON([]byte(`{"b":2,"a":{"d":4,"c":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := NormalizeJSON([]byte(`{"a":{"c":3,"d":4},"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Fatalf("normalization differs: %s != %s", left, right)
	}
}

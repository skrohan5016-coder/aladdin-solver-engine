package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestInvalidDeadlineRejected(t *testing.T) {
	response := post(t, testServer(t), "/solve", auctionJSON("not-a-deadline"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid deadline returned %d, want 400", response.Code)
	}
}

func TestTrailingAuctionJSONRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	response := post(t, testServer(t), "/solve", auctionJSON(deadline)+` {}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON returned %d, want 400", response.Code)
	}
}

func TestDuplicateTopLevelFieldRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := auctionJSON(deadline)
	payload = strings.Replace(
		payload,
		`"effectiveGasPrice": "1000000000"`,
		`"effectiveGasPrice": "1", "effectiveGasPrice": "1000000000"`,
		1,
	)
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate field returned %d, want 400", response.Code)
	}
}

func TestDuplicateNestedFieldRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := `{
		"id":"duplicate-nested",
		"tokens":{},
		"orders":[],
		"liquidity":[{
			"kind":"constantProduct",
			"id":"1",
			"address":"0x1",
			"gasEstimate":"90000",
			"fee":"0.003",
			"router":"0x2",
			"tokens":{
				"0xa":{"balance":"100","balance":"200"},
				"0xb":{"balance":"100"}
			}
		}],
		"effectiveGasPrice":"1",
		"deadline":"` + deadline + `",
		"surplusCapturingJitOrderOwners":[]
	}`
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("nested duplicate returned %d, want 400", response.Code)
	}
}

func TestOpaqueLiquidityIDAccepted(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := strings.Replace(auctionJSON(deadline), `"id": "7"`, `"id": "pool-seven"`, 1)
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusOK {
		t.Fatalf("opaque liquidity id returned %d, want 200: %s", response.Code, response.Body.String())
	}
}

func TestMissingRequiredAuctionCollectionRejected(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	payload := `{
		"tokens":{},
		"liquidity":[],
		"effectiveGasPrice":"1",
		"deadline":"` + deadline + `",
		"surplusCapturingJitOrderOwners":[]
	}`
	response := post(t, testServer(t), "/solve", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing orders returned %d, want 400", response.Code)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/solve"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	cfg := solve.DefaultConfig()
	cfg.RequireProfitable = false
	return New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), nil).Routes()
}

// A realistic auction payload in the exact shape the driver sends.
func auctionJSON(deadline string) string {
	return `{
	  "id": "42",
	  "tokens": {
	    "0x000000000000000000000000000000000000000a": {
	      "decimals": 18, "symbol": "AAA",
	      "referencePrice": "1000000000000000000",
	      "availableBalance": "0", "trusted": true
	    },
	    "0x000000000000000000000000000000000000000b": {
	      "decimals": 18, "symbol": "BBB",
	      "referencePrice": "1000000000000000000",
	      "availableBalance": "0", "trusted": true
	    }
	  },
	  "orders": [{
	    "uid": "0x11111111111111111111111111111111111111111111111111111111111111112222222222222222222222222222222222222222ffffffff",
	    "sellToken": "0x000000000000000000000000000000000000000a",
	    "buyToken": "0x000000000000000000000000000000000000000b",
	    "sellAmount": "1000000000000000000",
	    "fullSellAmount": "1000000000000000000",
	    "buyAmount": "1",
	    "fullBuyAmount": "1",
	    "validTo": 4102444800,
	    "kind": "sell",
	    "owner": "0x0000000000000000000000000000000000000001",
	    "partiallyFillable": false,
	    "preInteractions": [],
	    "postInteractions": [],
	    "sellTokenSource": "erc20",
	    "buyTokenDestination": "erc20",
	    "class": "market",
	    "appData": "0x0000000000000000000000000000000000000000000000000000000000000000",
	    "signingScheme": "eip712",
	    "signature": "0x00"
	  }],
	  "liquidity": [{
	    "kind": "constantProduct",
	    "tokens": {
	      "0x000000000000000000000000000000000000000a": {"balance": "1000000000000000000000"},
	      "0x000000000000000000000000000000000000000b": {"balance": "2000000000000000000000"}
	    },
	    "fee": "0.003",
	    "router": "0x0000000000000000000000000000000000000002",
	    "id": "7",
	    "address": "0x0000000000000000000000000000000000000003",
	    "gasEstimate": "90000"
	  }],
	  "effectiveGasPrice": "1000000000",
	  "deadline": "` + deadline + `",
	  "surplusCapturingJitOrderOwners": []
	}`
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestSolveEndToEnd(t *testing.T) {
	h := testServer(t)
	deadline := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	rr := post(t, h, "/solve", auctionJSON(deadline))

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}

	// Decode generically so the test asserts on the wire shape, not our structs.
	var out struct {
		Solutions []struct {
			ID           float64           `json:"id"`
			Prices       map[string]string `json:"prices"`
			Trades       []map[string]any  `json:"trades"`
			Interactions []map[string]any  `json:"interactions"`
			Gas          float64           `json:"gas"`
		} `json:"solutions"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid solver-engine JSON: %v\n%s", err, rr.Body)
	}
	if len(out.Solutions) != 1 {
		t.Fatalf("expected 1 solution, got %d: %s", len(out.Solutions), rr.Body)
	}
	s := out.Solutions[0]
	if len(s.Prices) != 2 {
		t.Errorf("expected a price for each traded token, got %v", s.Prices)
	}
	if len(s.Trades) != 1 || s.Trades[0]["kind"] != "fulfillment" {
		t.Errorf("unexpected trades: %v", s.Trades)
	}
	if len(s.Interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %v", s.Interactions)
	}
	if id, ok := s.Interactions[0]["id"].(string); !ok || id != "7" {
		t.Errorf("interaction id must preserve the opaque string 7, got %#v", s.Interactions[0]["id"])
	}
	if internalize, ok := s.Interactions[0]["internalize"].(bool); !ok || internalize {
		t.Errorf("internalize=false must be explicit, got %#v", s.Interactions[0]["internalize"])
	}
	if s.Gas == 0 {
		t.Error("solution should carry a gas estimate")
	}
}

func TestSolveReturnsEmptyArrayNotNull(t *testing.T) {
	h := testServer(t)
	body := `{"id":"1","tokens":{},"orders":[],"liquidity":[],
	          "effectiveGasPrice":"1000000000",
	          "deadline":"` + time.Now().Add(10*time.Second).UTC().Format(time.RFC3339) + `",
	          "surplusCapturingJitOrderOwners":[]}`
	rr := post(t, h, "/solve", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	// `"solutions": null` is not valid per the schema; it must be [].
	if got := rr.Body.String(); !bytes.Contains([]byte(got), []byte(`"solutions":[]`)) {
		t.Errorf("expected an empty array, got %s", got)
	}
}

func TestExpiredDeadlineReturnsEmpty(t *testing.T) {
	h := testServer(t)
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	rr := post(t, h, "/solve", auctionJSON(past))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var out struct {
		Solutions []json.RawMessage `json:"solutions"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Solutions) != 0 {
		t.Errorf("an expired deadline must yield no solutions, got %d", len(out.Solutions))
	}
}

func TestMalformedAuctionRejected(t *testing.T) {
	h := testServer(t)
	rr := post(t, h, "/solve", `{not json`)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestNotifyAlwaysAccepted(t *testing.T) {
	h := testServer(t)
	for _, body := range []string{
		`{"auctionId":"42","solutionId":0,"kind":"success"}`,
		`{"auctionId":"42","solutionId":1,"kind":"simulationFailed","extra":{"a":1}}`,
		`garbage`,
	} {
		if rr := post(t, h, "/notify", body); rr.Code != http.StatusOK {
			t.Errorf("notify %q returned %d, must always be 200", body, rr.Code)
		}
	}
}

func TestHealth(t *testing.T) {
	h := testServer(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Errorf("health returned %d", rr.Code)
	}
}

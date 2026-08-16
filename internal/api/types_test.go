package api

import (
	"encoding/json"
	"testing"
)

func TestNotificationPreservesUnknownMetadata(t *testing.T) {
	input := []byte(`{
		"auctionId":"42",
		"solutionId":7,
		"kind":"success",
		"rank":2,
		"details":{"objective":"12345678901234567890"}
	}`)
	var notification Notification
	if err := json.Unmarshal(input, &notification); err != nil {
		t.Fatal(err)
	}
	if notification.AuctionID != "42" || notification.SolutionID != 7 || notification.Kind != "success" {
		t.Fatalf("core fields changed: %+v", notification)
	}
	if len(notification.Extra) != 2 {
		t.Fatalf("expected two extensible fields, got %d", len(notification.Extra))
	}

	encoded, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"auctionId", "solutionId", "kind", "rank", "details"} {
		if _, ok := roundTrip[key]; !ok {
			t.Errorf("round trip lost %q: %s", key, encoded)
		}
	}
}

func TestNotificationUnmarshalReplacesPreviousState(t *testing.T) {
	notification := Notification{
		AuctionID:  "old",
		SolutionID: 9,
		Kind:       "old",
		Extra:      map[string]json.RawMessage{"stale": json.RawMessage(`true`)},
	}
	if err := json.Unmarshal([]byte(`{"auctionId":"new","kind":"success"}`), &notification); err != nil {
		t.Fatal(err)
	}
	if notification.AuctionID != "new" || notification.SolutionID != 0 || notification.Kind != "success" {
		t.Fatalf("old core state survived unmarshal: %+v", notification)
	}
	if _, ok := notification.Extra["stale"]; ok {
		t.Fatalf("old extension state survived unmarshal: %+v", notification.Extra)
	}
}

func TestInteractionIDMustBeDecimal(t *testing.T) {
	valid := Interaction{
		Kind: "liquidity", ID: "7", InputToken: "0xa", OutputToken: "0xb",
		InputAmount: "1", OutputAmount: "1",
	}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if id, ok := wire["id"].(float64); !ok || id != 7 {
		t.Fatalf("liquidity id is not a JSON number: %#v", wire["id"])
	}

	invalid := valid
	invalid.ID = "pool-seven"
	if _, err := json.Marshal(invalid); err == nil {
		t.Fatal("non-decimal liquidity id must not be emitted")
	}
}

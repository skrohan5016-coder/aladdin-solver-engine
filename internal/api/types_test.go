package api

import (
	"encoding/json"
	"testing"
)

func TestNotificationPreservesUnknownMetadata(t *testing.T) {
	input := []byte(`{
		"auctionId":"42",
		"solutionId":7,
		"kind":"settlementStarted",
		"rank":2,
		"details":{"objective":"12345678901234567890"}
	}`)
	var notification Notification
	if err := json.Unmarshal(input, &notification); err != nil {
		t.Fatal(err)
	}
	if notification.AuctionIDString() != "42" || notification.SolutionIDString() != "7" || notification.Kind != "settlementStarted" {
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

func TestNotificationPreservesMergedSolutionIDsAboveFloatRange(t *testing.T) {
	const largeID = "9007199254740993"
	input := []byte(`{"auctionId":"42","solutionId":[7,` + largeID + `],"kind":"settlementStarted"}`)
	var notification Notification
	if err := json.Unmarshal(input, &notification); err != nil {
		t.Fatal(err)
	}
	if notification.SolutionIDString() != "[7,"+largeID+"]" {
		t.Fatalf("solution ids changed: %q", notification.SolutionIDString())
	}
	encoded, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["solutionId"]) != "[7,"+largeID+"]" {
		t.Fatalf("encoded solution ids changed: %s", encoded)
	}
}

func TestNotificationAllowsOptionalIDsAndRequiresKind(t *testing.T) {
	for _, input := range []string{
		`{"kind":"timeout"}`,
		`{"auctionId":null,"solutionId":null,"kind":"timeout"}`,
		`{"auctionId":"-1","solutionId":[],"kind":"cancelled"}`,
	} {
		var notification Notification
		if err := json.Unmarshal([]byte(input), &notification); err != nil {
			t.Errorf("runtime-compatible notification was rejected: %s: %v", input, err)
		}
	}
	for _, input := range []string{
		`{"auctionId":"42","solutionId":1}`,
		`{"auctionId":42,"solutionId":1,"kind":"success","transaction":"0x0000000000000000000000000000000000000000000000000000000000000000"}`,
		`{"auctionId":"42","solutionId":"1","kind":"success","transaction":"0x0000000000000000000000000000000000000000000000000000000000000000"}`,
		`{"auctionId":"42","solutionId":[1,null],"kind":"success","transaction":"0x0000000000000000000000000000000000000000000000000000000000000000"}`,
		`{"auctionId":"42","solutionId":-1,"kind":"success","transaction":"0x0000000000000000000000000000000000000000000000000000000000000000"}`,
		`{"auctionId":"42","solutionId":1,"kind":null}`,
	} {
		var notification Notification
		if err := json.Unmarshal([]byte(input), &notification); err == nil {
			t.Errorf("invalid notification was accepted: %s", input)
		}
	}
}

func TestNotificationKindPayloadMatchesPinnedRuntimeDTO(t *testing.T) {
	valid := []string{
		`{"kind":"success","transaction":"0x0000000000000000000000000000000000000000000000000000000000000001"}`,
		`{"kind":"revert","transaction":"0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"}`,
		`{"kind":"missingPrice","tokenAddress":"0x0000000000000000000000000000000000000001"}`,
		`{"kind":"nonBufferableTokensUsed","tokens":[]}`,
		`{"kind":"solverAccountInsufficientBalance","required":"0xffff"}`,
		`{"kind":"driverError","reason":"driver rejected the candidate"}`,
		`{"kind":"deserializationError","reason":"bad payload"}`,
		`{"kind":"simulationFailed","block":1,"tx":{"from":"0x0000000000000000000000000000000000000001","to":"0x0000000000000000000000000000000000000002","input":"0x","value":"0","accessList":[]},"succeededOnce":false}`,
	}
	for _, input := range valid {
		var notification Notification
		if err := json.Unmarshal([]byte(input), &notification); err != nil {
			t.Errorf("runtime-compatible notification was rejected: %s: %v", input, err)
		}
	}

	invalid := []string{
		`{"kind":"success"}`,
		`{"kind":"success","transaction":"0x01"}`,
		`{"kind":"missingPrice","tokenAddress":"0x01"}`,
		`{"kind":"nonBufferableTokensUsed","tokens":null}`,
		`{"kind":"solverAccountInsufficientBalance","required":1}`,
		`{"kind":"driverError"}`,
		`{"kind":"simulationFailed","block":1,"tx":{},"succeededOnce":false}`,
		`{"kind":"notInPinnedRuntime"}`,
	}
	for _, input := range invalid {
		var notification Notification
		if err := json.Unmarshal([]byte(input), &notification); err == nil {
			t.Errorf("invalid runtime notification was accepted: %s", input)
		}
	}
}

func TestNotificationUnmarshalReplacesPreviousState(t *testing.T) {
	oldAuction := "old"
	notification := Notification{
		AuctionID:  &oldAuction,
		SolutionID: NewSingleNotificationSolutionID(9),
		Kind:       "old",
		Extra:      map[string]json.RawMessage{"stale": json.RawMessage(`true`)},
	}
	if err := json.Unmarshal([]byte(`{"kind":"settlementStarted"}`), &notification); err != nil {
		t.Fatal(err)
	}
	if notification.AuctionID != nil || notification.SolutionID != nil || notification.Kind != "settlementStarted" {
		t.Fatalf("old core state survived unmarshal: %+v", notification)
	}
	if _, ok := notification.Extra["stale"]; ok {
		t.Fatalf("old extension state survived unmarshal: %+v", notification.Extra)
	}
}

func TestInteractionPreservesOpaqueIDAndExplicitInternalize(t *testing.T) {
	interaction := Interaction{
		Kind: "liquidity", ID: "pool/opaque/7", InputToken: "0xa", OutputToken: "0xb",
		InputAmount: "1", OutputAmount: "1", Internalize: false,
	}
	encoded, err := json.Marshal(interaction)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if id, ok := wire["id"].(string); !ok || id != interaction.ID {
		t.Fatalf("liquidity id was not preserved as an opaque string: %#v", wire["id"])
	}
	if internalize, ok := wire["internalize"].(bool); !ok || internalize {
		t.Fatalf("internalize=false was omitted or changed: %#v", wire["internalize"])
	}
}

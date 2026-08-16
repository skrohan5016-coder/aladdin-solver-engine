package server

import (
	"net/http"
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

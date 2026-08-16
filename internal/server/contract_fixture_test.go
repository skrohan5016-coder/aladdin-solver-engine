package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
)

func TestPinnedAuctionFixtureProducesPinnedSolutionFixture(t *testing.T) {
	auction, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "auction-direct.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "solution-direct.json"))
	if err != nil {
		t.Fatal(err)
	}
	response := post(t, testServer(t), "/solve", string(auction))
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	actualNormalized, err := contract.NormalizeJSON(response.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	expectedNormalized, err := contract.NormalizeJSON(expected)
	if err != nil {
		t.Fatal(err)
	}
	if string(actualNormalized) != string(expectedNormalized) {
		t.Fatalf("fixture replay changed\nactual: %s\nexpected: %s", actualNormalized, expectedNormalized)
	}
}

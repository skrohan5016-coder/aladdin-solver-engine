package solve

import "testing"

func TestConfigSnapshotIsStableAndResolved(t *testing.T) {
	config := Config{RequireProfitable: false, MaxSolutions: 0}
	first, firstDigest, err := SnapshotConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	second, secondDigest, err := SnapshotConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || firstDigest != secondDigest {
		t.Fatalf("config identity changed: %#v %q != %#v %q", first, firstDigest, second, secondDigest)
	}
	if first.MaxOrders != DefaultConfig().MaxOrders || first.MaxPools != DefaultConfig().MaxPools {
		t.Fatalf("defaults were not resolved: %#v", first)
	}
	roundTrip, err := first.Config()
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip != ResolveConfig(config) {
		t.Fatalf("config round trip changed: %#v", roundTrip)
	}
}

func TestConfigSnapshotRejectsNegativeSolutionLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxSolutions = -1
	if _, _, err := SnapshotConfig(config); err == nil {
		t.Fatal("negative max solutions was accepted")
	}
}

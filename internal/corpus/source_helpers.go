package corpus

import "github.com/skrohan5016-coder/aladdin-solver-engine/internal/record"

type recordIdentity struct {
	record.RunIdentity
}

func decodeSourceRecord(line []byte) (record.AuctionRecord, error) {
	var source record.AuctionRecord
	if err := decodeStrict(line, &source); err != nil {
		return record.AuctionRecord{}, err
	}
	return source, nil
}

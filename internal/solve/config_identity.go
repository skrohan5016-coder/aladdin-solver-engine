package solve

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
)

const ConfigSchema = "aladdin-solver-config/v1"

// ConfigSnapshot is the canonical, versioned identity of every solver knob
// capable of changing replay output.
type ConfigSnapshot struct {
	Schema                string `json:"schema"`
	SettlementOverheadGas uint64 `json:"settlementOverheadGas"`
	PerTradeGas           uint64 `json:"perTradeGas"`
	RequireProfitable     bool   `json:"requireProfitable"`
	MaxSolutions          int    `json:"maxSolutions"`
	MaxOrders             int    `json:"maxOrders"`
	MaxPools              int    `json:"maxPools"`
}

func ResolveConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.MaxOrders <= 0 {
		config.MaxOrders = defaults.MaxOrders
	}
	if config.MaxPools <= 0 {
		config.MaxPools = defaults.MaxPools
	}
	return config
}

func SnapshotConfig(config Config) (ConfigSnapshot, string, error) {
	config = ResolveConfig(config)
	if config.MaxSolutions < 0 {
		return ConfigSnapshot{}, "", errors.New("max solutions is negative")
	}
	if config.MaxOrders <= 0 || config.MaxPools <= 0 {
		return ConfigSnapshot{}, "", errors.New("resolved order and pool limits must be positive")
	}
	snapshot := ConfigSnapshot{
		Schema:                ConfigSchema,
		SettlementOverheadGas: config.SettlementOverheadGas,
		PerTradeGas:           config.PerTradeGas,
		RequireProfitable:     config.RequireProfitable,
		MaxSolutions:          config.MaxSolutions,
		MaxOrders:             config.MaxOrders,
		MaxPools:              config.MaxPools,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return ConfigSnapshot{}, "", fmt.Errorf("encode config snapshot: %w", err)
	}
	return snapshot, fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}

func (snapshot ConfigSnapshot) Config() (Config, error) {
	if snapshot.Schema != ConfigSchema {
		return Config{}, fmt.Errorf("unsupported config schema %q", snapshot.Schema)
	}
	config := Config{
		SettlementOverheadGas: snapshot.SettlementOverheadGas,
		PerTradeGas:           snapshot.PerTradeGas,
		RequireProfitable:     snapshot.RequireProfitable,
		MaxSolutions:          snapshot.MaxSolutions,
		MaxOrders:             snapshot.MaxOrders,
		MaxPools:              snapshot.MaxPools,
	}
	resolved := ResolveConfig(config)
	if resolved != config {
		return Config{}, errors.New("config snapshot is not fully resolved")
	}
	if _, _, err := SnapshotConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

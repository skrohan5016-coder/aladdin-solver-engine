// Package buildinfo exposes the immutable source and toolchain identity embedded
// in solver, replay, and reporting binaries.
package buildinfo

import "runtime"

// Commit is populated through -ldflags at build time. Development builds may
// retain "unknown", but full-auction recording, corpus packing, and replay
// verification fail closed unless the running binary itself embeds an exact
// lowercase 40-hex source commit. A command-line assertion cannot replace it.
var Commit = "unknown"

type Identity struct {
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

func Current() Identity {
	return Identity{
		Commit:    Commit,
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
}

func ValidCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for index := range value {
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

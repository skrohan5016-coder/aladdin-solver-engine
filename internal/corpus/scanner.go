package corpus

import (
	"bufio"
	"bytes"
)

func scannerForBytes(data []byte, maxLineBytes int) *bufio.Scanner {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	initial := 64 << 10
	if maxLineBytes < initial {
		initial = maxLineBytes
	}
	scanner.Buffer(make([]byte, initial), maxLineBytes)
	return scanner
}

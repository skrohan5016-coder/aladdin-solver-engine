package corpus

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/skrohan5016-coder/aladdin-solver-engine/internal/contract"
)

func decodeStrict(data []byte, target any) error {
	if err := contract.ValidateUniqueJSON(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple top-level JSON values")
		}
		return err
	}
	return nil
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	normalized, err := contract.NormalizeJSON(data)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(normalized, []byte("\n")), nil
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func safeBaseName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) ||
		filepath.Clean(name) != name || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("unsafe corpus path %q", name)
	}
	return nil
}

func secureDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(absolute)
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("path is not a real directory: %s", clean)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	if filepath.Clean(resolved) != clean {
		return "", fmt.Errorf("directory contains a symlink boundary: %s", clean)
	}
	return clean, nil
}

func regularFile(path string, maxBytes int64) ([]byte, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return nil, nil, fmt.Errorf("file exceeds byte limit: %s (%d > %d)", path, info.Size(), maxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) != info.Size() {
		return nil, nil, fmt.Errorf("file size changed while reading: %s", path)
	}
	return data, info, nil
}

func openRegular(path string, maxBytes int64) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return nil, nil, fmt.Errorf("file exceeds byte limit: %s (%d > %d)", path, info.Size(), maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

func scannerFor(file *os.File, maxLineBytes int) *bufio.Scanner {
	scanner := bufio.NewScanner(file)
	initial := 64 << 10
	if maxLineBytes < initial {
		initial = maxLineBytes
	}
	scanner.Buffer(make([]byte, initial), maxLineBytes)
	return scanner
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func WriteJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := secureDirectory(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".replay-report-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	written, writeErr := temporary.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(temporaryPath, absolute)
}

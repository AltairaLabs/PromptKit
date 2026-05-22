package file

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// scanLineMax is the maximum SSE-style line size accepted when streaming
// JSONL files. Some messages contain tool-result blobs that exceed the default
// 64 KiB bufio limit; beyond 8 MiB JSONL is the wrong storage format.
const scanLineMax = 8 * 1024 * 1024

// writeFileAtomic writes data to a temp file in the same directory then
// renames it over dst. With fsync >= FSyncOnSave, the temp file is fsynced
// before the rename so the rename can't outrun the data flush.
func writeFileAtomic(dst string, data []byte, fsync FSyncPolicy) error {
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return fmt.Errorf("filestore: mkdir parent: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return fmt.Errorf("filestore: create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op when rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("filestore: write tmp: %w", err)
	}
	if fsync >= FSyncOnSave {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("filestore: fsync tmp: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("filestore: close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("filestore: rename: %w", err)
	}
	return nil
}

// appendJSONLine marshals v and appends it to path as a single line.
// Creates parent dirs and the file as needed.
func appendJSONLine(path string, v any, fsync FSyncPolicy) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("filestore: marshal jsonl: %w", err)
	}
	return appendJSONBytes(path, data, fsync)
}

// appendJSONBytes appends pre-marshaled JSON bytes plus a newline.
func appendJSONBytes(path string, data []byte, fsync FSyncPolicy) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("filestore: mkdir parent: %w", err)
	}
	//nolint:gosec // path is built from store-controlled convDir + constant filenames
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, fileMode)
	if err != nil {
		return fmt.Errorf("filestore: open append: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("filestore: append write: %w", err)
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("filestore: append newline: %w", err)
	}
	if fsync >= FSyncOnAppend {
		if err := f.Sync(); err != nil {
			return fmt.Errorf("filestore: fsync append: %w", err)
		}
	}
	return nil
}

// scanJSONLines streams lines from path, decoding each into a fresh T.
// Returns (nil, nil) when the file doesn't exist. Stops at the first decode
// error.
func scanJSONLines[T any](path string) ([]T, error) {
	//nolint:gosec // path is built from store-controlled convDir + constant filenames
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("filestore: open scan: %w", err)
	}
	defer func() { _ = f.Close() }()

	out := []T{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), scanLineMax)
	for scanner.Scan() {
		var v T
		if err := json.Unmarshal(scanner.Bytes(), &v); err != nil {
			return nil, fmt.Errorf("filestore: decode jsonl: %w", err)
		}
		out = append(out, v)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("filestore: scan jsonl: %w", err)
	}
	return out, nil
}

// countLines returns the number of \n-terminated lines in path, treating a
// final line without trailing newline as one line. 0 when the file is absent.
func countLines(path string) (int, error) {
	//nolint:gosec // path is built from store-controlled convDir + constant filenames
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("filestore: open count: %w", err)
	}
	defer func() { _ = f.Close() }()

	var (
		buf           [bufio.MaxScanTokenSize]byte
		count         int
		endsWithNewLF = true // true ↔ file is empty OR ends in '\n'
	)
	for {
		n, err := f.Read(buf[:])
		if n > 0 {
			chunk := buf[:n]
			count += bytes.Count(chunk, []byte{'\n'})
			endsWithNewLF = chunk[len(chunk)-1] == '\n'
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("filestore: read count: %w", err)
		}
	}
	if !endsWithNewLF {
		count++
	}
	return count, nil
}

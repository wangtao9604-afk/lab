package fetcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// CursorRecord represents the persisted local shadow of cursor progression.
type CursorRecord struct {
	AppID     string `json:"app_id"`
	Epoch     int64  `json:"epoch"`
	Version   int64  `json:"version"`
	Cursor    string `json:"cursor"`
	Dirty     bool   `json:"dirty"`
	UpdatedAt int64  `json:"updated_at"`
}

// FileCursorStore atomically persists the cursor record to disk.
type FileCursorStore struct {
	path string
}

// NewFileCursorStore creates a new file-based cursor store targeting the given path.
func NewFileCursorStore(path string) *FileCursorStore {
	return &FileCursorStore{path: path}
}

// Path returns the underlying file path.
func (s *FileCursorStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the persisted record. Returns (nil, nil) if the file does not exist.
func (s *FileCursorStore) Load(ctx context.Context) (*CursorRecord, error) {
	if s == nil {
		return nil, nil
	}
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open shadow cursor: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read shadow cursor: %w", err)
	}
	var rec CursorRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("decode shadow cursor: %w", err)
	}
	return &rec, nil
}

// Save writes the record atomically.
func (s *FileCursorStore) Save(ctx context.Context, rec *CursorRecord) error {
	if s == nil || rec == nil {
		return nil
	}

	tmp := s.path + ".tmp"
	dir := filepath.Dir(s.path)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir shadow dir: %w", err)
	}

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp shadow: %w", err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	rec.UpdatedAt = time.Now().Unix()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rec); err != nil {
		return fmt.Errorf("encode shadow cursor: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync tmp shadow: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp shadow: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename shadow cursor: %w", err)
	}

	if df, err := os.Open(dir); err == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}

//go:build ignore

package localstore

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

// Record 为本地 cursor 影子文件的结构
type Record struct {
	AppID     string `json:"app_id"`
	Epoch     int64  `json:"epoch"`      // 当选时的围栏令牌（用于审计），不作为比较依据
	Version   int64  `json:"version"`    // 本地版本号（单调+1），仅用于“谁更新更晚”的比较
	Cursor    string `json:"cursor"`     // 最新 cursor 值（直接透传上游返回）
	Dirty     bool   `json:"dirty"`      // 上次保存时远端是否不可达（true=未同步至远端）
	UpdatedAt int64  `json:"updated_at"` // Unix 秒
}

// FileCursorStore 管理本地影子文件的 Save/Load（原子写入 + 目录 fsync）
type FileCursorStore struct {
	path string
}

// NewFileCursorStore 创建本地存储
func NewFileCursorStore(path string) *FileCursorStore {
	return &FileCursorStore{path: path}
}

// Load 读取本地记录（若文件不存在返回 (nil,nil) 表示无本地影子）
func (s *FileCursorStore) Load(ctx context.Context) (*Record, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &r, nil
}

// Save 原子写入（tmp→fsync→close→rename→dir fsync）
// 说明：
// - 顶部使用 defer-close 仅用于“异常提前返回”时兜底；
// - 成功路径上会显式 Close()，随后将 f 置为 nil，避免 defer 二次关闭。
func (s *FileCursorStore) Save(ctx context.Context, r *Record) error {
	tmp := s.path + ".tmp"
	dir := filepath.Dir(s.path)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	// defer 仅兜底早退；成功路径会显式 Close 并把 f 置 nil，避免二次关闭
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()

	r.UpdatedAt = time.Now().Unix()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync tmp: %w", err)
	}
	// 显式关闭，确保 Windows 下可重命名（释放句柄）
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	f = nil // ← 关键：避免 defer 再次 Close()

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	// 目录 fsync，确保 rename 持久化（某些平台可能不支持，忽略错误）
	if df, err := os.Open(dir); err == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}

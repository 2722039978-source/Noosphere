// Package contract defines the workspace boundary used by the Agent engine.
package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"sort"
)

var ErrWorkspaceChanged = errors.New("workspace changed since verification")

type Change struct {
	Path       string      `json:"path"`
	Kind       string      `json:"kind"`
	IsDir      bool        `json:"is_dir,omitempty"`
	BeforeHash string      `json:"before_hash,omitempty"`
	AfterHash  string      `json:"after_hash,omitempty"`
	BeforeMode fs.FileMode `json:"before_mode,omitempty"`
	AfterMode  fs.FileMode `json:"after_mode,omitempty"`
	BeforeSize int64       `json:"before_size,omitempty"`
	Size       int64       `json:"size"`
	Diff       string      `json:"diff,omitempty"`
}

func ManifestDigest(changes []Change) string {
	items := make([]Change, len(changes))
	copy(items, changes)
	for i := range items {
		items[i].Diff = ""
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	b, _ := json.Marshal(items)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

type FileConflict struct {
	Path         string `json:"path"`
	BeforeHash   string `json:"before_hash,omitempty"`
	ProposalHash string `json:"proposal_hash,omitempty"`
	CurrentHash  string `json:"current_hash,omitempty"`
	Diff         string `json:"diff,omitempty"`
}
type RefreshResult struct {
	ChangedInputs int            `json:"changed_inputs"`
	Conflicts     []FileConflict `json:"conflicts"`
}

// Workspace exposes versioned draft operations. Host paths, credentials,
// processes and storage implementation stay in the desktop adapter.
type Workspace interface {
	List(context.Context) ([]string, error)
	Stat(context.Context, string) (int64, string, error)
	Read(context.Context, string, int64, int) ([]byte, error)
	Replace(context.Context, string, string, []byte) error
	Delete(context.Context, string) error
	Move(context.Context, string, string, string) error
	Changes(context.Context) ([]Change, error)
	Publish(context.Context, string) error
	Status(context.Context) (string, error)
	Refresh(context.Context) (RefreshResult, error)
	FileConflicts(context.Context) ([]FileConflict, error)
	ReadConflict(context.Context, string, string, int64, int) ([]byte, error)
}

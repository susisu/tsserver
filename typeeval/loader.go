package typeeval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxSnapshotFiles = 10_000
	maxSnapshotBytes = 64 << 20 // 64 MiB
	maxFileBytes     = 8 << 20  // 8 MiB
)

// Load snapshots an app directory from the OS filesystem and builds an
// Evaluator from it. Only TypeScript sources and package.json files are
// read; symlinks are followed (so pnpm-style node_modules work) and entries
// starting with "." are skipped.
func Load(ctx context.Context, dir string) (*Evaluator, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("typeeval: resolve app directory: %w", err)
	}
	w := &walker{
		files: make(map[string]string),
		seen:  make(map[string]bool),
	}
	if err := w.walk(root, ""); err != nil {
		return nil, fmt.Errorf("typeeval: load app directory: %w", err)
	}
	return New(ctx, w.files)
}

type walker struct {
	files      map[string]string // vfs-relative slash path -> contents
	seen       map[string]bool   // resolved directory paths, for cycle protection
	totalBytes int64
}

func (w *walker) walk(osDir, relDir string) error {
	resolved, err := filepath.EvalSymlinks(osDir)
	if err != nil {
		return err
	}
	if w.seen[resolved] {
		return nil
	}
	w.seen[resolved] = true

	entries, err := os.ReadDir(osDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		osPath := filepath.Join(osDir, name)
		relPath := name
		if relDir != "" {
			relPath = relDir + "/" + name
		}
		// Stat (not the dirent) so that symlinks to dirs/files are followed.
		info, err := os.Stat(osPath)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := w.walk(osPath, relPath); err != nil {
				return err
			}
		case info.Mode().IsRegular() && wantFile(name):
			if info.Size() > maxFileBytes {
				return fmt.Errorf("%s exceeds %d bytes", relPath, int64(maxFileBytes))
			}
			contents, err := os.ReadFile(osPath)
			if err != nil {
				return err
			}
			w.totalBytes += int64(len(contents))
			if w.totalBytes > maxSnapshotBytes {
				return fmt.Errorf("app snapshot exceeds %d bytes", int64(maxSnapshotBytes))
			}
			w.files[relPath] = string(contents)
			if len(w.files) > maxSnapshotFiles {
				return errors.New("app snapshot exceeds file count limit")
			}
		}
	}
	return nil
}

func wantFile(name string) bool {
	return name == "package.json" ||
		strings.HasSuffix(name, ".ts") ||
		strings.HasSuffix(name, ".mts") ||
		strings.HasSuffix(name, ".cts")
}

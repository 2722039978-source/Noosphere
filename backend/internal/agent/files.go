package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

var extensions = map[string]bool{".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".rs": true, ".java": true, ".cs": true, ".cpp": true, ".c": true, ".h": true, ".vue": true, ".svelte": true, ".html": true, ".css": true, ".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true, ".sql": true, ".ps1": true, ".bat": true, ".sh": true}
var excluded = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, "release": true, "__pycache__": true, "venv": true, "bin": true, "obj": true}

func allowedPath(name string) bool {
	if !fs.ValidPath(name) || len(name) > 512 || strings.ContainsAny(name, "\\:") {
		return false
	}
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if strings.HasPrefix(part, ".") || excluded[part] || strings.Contains(part, "credential") || strings.Contains(part, "secret") || part == "models.yaml" || part == "models.yml" || part == "package-lock.json" || part == "pnpm-lock.yaml" {
			return false
		}
	}
	return extensions[strings.ToLower(path.Ext(name))] || path.Base(name) == "Dockerfile"
}
func (r *Registry) read(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !allowedPath(name) {
		return nil, fmt.Errorf("path is not an eligible project source file")
	}
	if r.workspace.ProjectPath == "" {
		return nil, fmt.Errorf("请先设置工作区项目目录")
	}
	root, err := os.OpenRoot(r.workspace.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("project directory unavailable")
	}
	defer root.Close()
	// Refuse symlink components, including in-root links to normally excluded files.
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, fmt.Errorf("source file unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("linked source paths are not supported")
		}
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("source file unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, fmt.Errorf("source must be a regular text file up to 1 MiB")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1024*1024 || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, fmt.Errorf("source is too large or not UTF-8 text")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
func fileEvidence(name string, raw []byte, start, end int) (Evidence, error) {
	lines := strings.Split(string(raw), "\n")
	if start < 1 || start > len(lines) {
		return Evidence{}, fmt.Errorf("start line outside file")
	}
	end = min(end, len(lines))
	content := strings.Join(lines[start-1:end], "\n")
	return Evidence{Kind: "code", Title: name, File: name, StartLine: start, EndLine: end, SourceHash: fmt.Sprintf("%x", sha256.Sum256(raw)), Excerpt: clip(redact(content), 5000), ObservedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}
func (r *Registry) scan(ctx context.Context, query string) Outcome {
	if r.workspace.ProjectPath == "" {
		return fail(fmt.Errorf("请先设置工作区项目目录"))
	}
	root, err := os.OpenRoot(r.workspace.ProjectPath)
	if err != nil {
		return fail(fmt.Errorf("project directory unavailable"))
	}
	defer root.Close()
	out := Outcome{Success: true}
	count, total := 0, 0
	var names []string
	partial := false
	needle := strings.ToLower(query)
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 10000 || total > 16*1024*1024 {
			partial = true
			return fs.SkipAll
		}
		if walkErr != nil {
			partial = true
			return nil
		}
		if name == "." {
			return nil
		}
		if d.IsDir() {
			base := strings.ToLower(d.Name())
			if strings.HasPrefix(base, ".") || excluded[base] {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !allowedPath(name) {
			return nil
		}
		if query == "" {
			names = append(names, name)
			if len(names) >= 200 {
				partial = true
				return fs.SkipAll
			}
			return nil
		}
		raw, err := r.read(ctx, name)
		if err != nil {
			partial = true
			return nil
		}
		total += len(raw)
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				e, _ := fileEvidence(name, raw, max(1, i), i+2)
				e.Excerpt = clip(e.Excerpt, 1500)
				out.Evidence = append(out.Evidence, e)
				break
			}
		}
		if len(out.Evidence) >= 5 {
			partial = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if query == "" {
		out.Output = clip(strings.Join(names, "\n"), 8000)
	} else {
		out.Output = fmt.Sprintf("Found %d matching source excerpts.", len(out.Evidence))
	}
	if partial {
		out.Output += "\n[partial scan: result, file or size limit reached; some paths may be unavailable]"
	}
	return out
}

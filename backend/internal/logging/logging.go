package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

func Init(dir string) (io.Closer, error) {
	if dir == "" {
		dir = "./logs"
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}

	name := fmt.Sprintf("noosphere-%s.log", time.Now().Format("2006-01-02"))
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}

	// Write to both file and stderr
	w := io.MultiWriter(f, os.Stderr)
	log.SetOutput(w)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return f, nil
}

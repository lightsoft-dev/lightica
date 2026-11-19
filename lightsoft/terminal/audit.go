package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// auditLog appends one JSON line per terminal open, close and denial. It
// never records terminal input or output: both can carry secrets.
type auditLog struct {
	mu   sync.Mutex
	path string
}

func (a *auditLog) write(event string, fields map[string]any) {
	rec := map[string]any{"ts": time.Now().Format(time.RFC3339), "event": event}
	for k, v := range fields {
		rec[k] = v
	}
	line, err := json.Marshal(rec)
	if err != nil {
		log.Printf("audit: marshal: %v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		log.Printf("audit: %v", err)
		return
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("audit: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		log.Printf("audit: %v", err)
	}
}

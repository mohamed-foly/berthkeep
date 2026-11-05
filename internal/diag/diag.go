// Package diag writes diagnostics for bug reports.
package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"
)

// WriteStacks saves every goroutine's stack to <home>/logs and returns the file.
func WriteStacks(home, reason string) (string, error) {
	dir := filepath.Join(home, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("stacks-%s-%s.txt", reason, time.Now().Format("20060102-150405")))
	f, err := os.Create(p) //nolint:gosec // path built from our own home
	if err != nil {
		return "", err
	}
	defer f.Close()
	fmt.Fprintf(f, "reason: %s\ntime: %s\n\n", reason, time.Now().Format(time.RFC3339))
	if err := pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		return "", err
	}
	return p, nil
}

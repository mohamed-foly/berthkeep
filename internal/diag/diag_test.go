package diag

import (
	"os"
	"strings"
	"testing"
)

func TestWriteStacks(t *testing.T) {
	p, err := WriteStacks(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "goroutine") {
		t.Fatalf("no stacks in %s", p)
	}
}

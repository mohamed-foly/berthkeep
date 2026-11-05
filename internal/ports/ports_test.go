package ports

import (
	"errors"
	"net"
	"testing"
)

func TestAllocateSkipsReservedAndBusy(t *testing.T) {
	busy := map[int]bool{9223: true}
	got, err := Allocate(9222, 9230, map[int]bool{9222: true}, func(p int) bool { return !busy[p] })
	if err != nil || got != 9224 {
		t.Fatalf("got %d %v, want 9224", got, err)
	}
	if _, err := Allocate(9222, 9223, map[int]bool{9222: true}, func(p int) bool { return !busy[p] }); !errors.Is(err, ErrExhausted) {
		t.Fatalf("want exhausted, got %v", err)
	}
}

func TestListeningAndBindable(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	p := l.Addr().(*net.TCPAddr).Port
	if !IsListening(p) || IsBindable(p) || IsFree(p) {
		t.Fatalf("port %d should be busy", p)
	}
	l.Close()
	if IsListening(p) || !IsFree(p) {
		t.Fatalf("port %d should be free after close", p)
	}
}

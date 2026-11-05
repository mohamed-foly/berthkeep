// Package ports checks and allocates TCP ports on the loopback interface.
package ports

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

const loopback = "127.0.0.1"

// IsListening reports whether something accepts connections on 127.0.0.1:port.
func IsListening(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(loopback, strconv.Itoa(port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// IsBindable reports whether we could listen on 127.0.0.1:port right now.
func IsBindable(port int) bool {
	l, err := net.Listen("tcp", net.JoinHostPort(loopback, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// IsFree combines both checks: nothing listening and bindable.
func IsFree(port int) bool { return !IsListening(port) && IsBindable(port) }

// ErrExhausted means no free port is left in the range.
var ErrExhausted = errors.New("no free port in range")

// Allocate returns the lowest free, unreserved port.
func Allocate(start, end int, reserved map[int]bool, free func(int) bool) (int, error) {
	if free == nil {
		free = IsFree
	}
	for p := start; p <= end; p++ {
		if reserved[p] {
			continue
		}
		if free(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("%w %d-%d", ErrExhausted, start, end)
}

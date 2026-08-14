// Package isolate contains decoder panics.
//
// Principle II requires that no decoder failure can cost a byte, abort a
// session or break a relay. The journal architecture already guarantees the
// first — decode runs downstream of durable bytes — but a panic would still
// take down the process and with it the capture. This package is the second
// line: a panic becomes a recorded failure on one record and nothing more.
package isolate

import (
	"fmt"
	"runtime/debug"
)

// Recovered describes a contained panic.
type Recovered struct {
	Value any
	Stack []byte
}

func (r *Recovered) Error() string {
	return fmt.Sprintf("decoder panicked: %v", r.Value)
}

// Run calls fn, converting a panic into an error.
//
// The stack is captured because a decoder panic is a bug report: the record
// that triggered it is in the journal, so it can be replayed against a fixed
// decoder later. Losing the stack would waste that.
func Run(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Recovered{Value: r, Stack: debug.Stack()}
		}
	}()
	return fn()
}

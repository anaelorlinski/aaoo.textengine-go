// Package test holds the few assertion helpers the copied font tests use.
package test

import (
	"fmt"
	"testing"
)

func message(msgs []any) string {
	if len(msgs) == 0 {
		return ""
	}
	return fmt.Sprintln(msgs...)
}

// T fails the test when got differs from wanted.
func T[V comparable](t *testing.T, got, wanted V, msgs ...any) {
	t.Helper()
	if got != wanted {
		t.Errorf("%sgot %v, want %v", message(msgs), got, wanted)
	}
}

// Error fails the test when err is not nil.
func Error(t *testing.T, err error, msgs ...any) {
	t.Helper()
	if err != nil {
		t.Errorf("%sunexpected error: %v", message(msgs), err)
	}
}

// That fails the test when the condition does not hold.
func That(t *testing.T, condition bool, msgs ...any) {
	t.Helper()
	if !condition {
		t.Errorf("%scondition does not hold", message(msgs))
	}
}

// Fail fails the test.
func Fail(t *testing.T, msgs ...any) {
	t.Helper()
	t.Error(message(msgs))
}

package web

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(2, time.Minute)
	now := time.Now()

	if !l.allow("a", now) || !l.allow("a", now) {
		t.Fatal("first two requests should pass")
	}
	if l.allow("a", now) {
		t.Fatal("third request should be blocked")
	}
	if !l.allow("b", now) {
		t.Fatal("other keys are independent")
	}
	if !l.allow("a", now.Add(2*time.Minute)) {
		t.Fatal("window should reset")
	}
}

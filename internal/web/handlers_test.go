package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		forwarded  string
		remoteAddr string
		want       string
	}{
		{name: "forwarded first address", forwarded: "203.0.113.10, 198.51.100.7", remoteAddr: "10.0.0.1:1234", want: "203.0.113.10"},
		{name: "forwarded trims spaces", forwarded: " 203.0.113.10 ", remoteAddr: "10.0.0.1:1234", want: "203.0.113.10"},
		{name: "remote host port", remoteAddr: "10.0.0.1:1234", want: "10.0.0.1"},
		{name: "raw remote addr fallback", remoteAddr: "not-host-port", want: "not-host-port"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}

			if got := clientIdentifier(req); got != tt.want {
				t.Fatalf("clientIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactPhone(t *testing.T) {
	tests := []struct {
		phone string
		want  string
	}{
		{phone: "", want: ""},
		{phone: "+123", want: "+123"},
		{phone: "+420123456789", want: "+42********89"},
	}

	for _, tt := range tests {
		t.Run(tt.phone, func(t *testing.T) {
			if got := redactPhone(tt.phone); got != tt.want {
				t.Fatalf("redactPhone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoginLimiterAllowsUntilFailureLimit(t *testing.T) {
	limiter := &loginLimiter{attempts: map[string]loginAttempt{}}
	clientID := "203.0.113.10"

	for i := 0; i < maxLoginFailures-1; i++ {
		limiter.fail(clientID)
		if _, blocked := limiter.allow(clientID); blocked {
			t.Fatalf("allow blocked after %d failures, want not blocked", i+1)
		}
	}

	limiter.fail(clientID)
	retryAfter, blocked := limiter.allow(clientID)
	if !blocked {
		t.Fatal("allow did not block after max failures")
	}
	if retryAfter == "" {
		t.Fatal("retryAfter is empty")
	}
}

func TestLoginLimiterReset(t *testing.T) {
	limiter := &loginLimiter{attempts: map[string]loginAttempt{}}
	clientID := "203.0.113.10"

	for i := 0; i < maxLoginFailures; i++ {
		limiter.fail(clientID)
	}
	limiter.reset(clientID)

	if _, blocked := limiter.allow(clientID); blocked {
		t.Fatal("allow blocked after reset")
	}
	if _, ok := limiter.attempts[clientID]; ok {
		t.Fatal("reset did not remove attempt")
	}
}

func TestLoginLimiterPrunesStaleAttempts(t *testing.T) {
	limiter := &loginLimiter{attempts: map[string]loginAttempt{
		"stale": {
			count:   1,
			updated: time.Now().Add(-loginAttemptTTL - time.Minute),
		},
		"fresh": {
			count:   1,
			updated: time.Now(),
		},
		"blocked": {
			count:   maxLoginFailures,
			blocked: time.Now().Add(time.Minute),
			updated: time.Now().Add(-loginAttemptTTL - time.Minute),
		},
	}}

	limiter.pruneLocked()

	if _, ok := limiter.attempts["stale"]; ok {
		t.Fatal("stale attempt was not pruned")
	}
	if _, ok := limiter.attempts["fresh"]; !ok {
		t.Fatal("fresh attempt was pruned")
	}
	if _, ok := limiter.attempts["blocked"]; !ok {
		t.Fatal("currently blocked attempt was pruned")
	}
}

func TestLoginLimiterAllowsAfterBlockExpires(t *testing.T) {
	limiter := &loginLimiter{attempts: map[string]loginAttempt{
		"client": {
			count:   maxLoginFailures,
			blocked: time.Now().Add(-time.Second),
			updated: time.Now(),
		},
	}}

	if _, blocked := limiter.allow("client"); blocked {
		t.Fatal("allow blocked after block expiry")
	}
}

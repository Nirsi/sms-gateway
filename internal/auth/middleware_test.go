package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestIsSecureRequest(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{name: "nil request", req: nil, want: false},
		{name: "tls", req: &http.Request{TLS: &tls.ConnectionState{}}, want: true},
		{name: "forwarded proto header", req: requestWithHeader("X-Forwarded-Proto", "HTTPS"), want: true},
		{name: "forwarded header", req: requestWithHeader("Forwarded", "for=192.0.2.60;proto=https;by=203.0.113.43"), want: true},
		{name: "plain", req: httptest.NewRequest(http.MethodGet, "/", nil), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSecureRequest(tt.req); got != tt.want {
				t.Fatalf("IsSecureRequest() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestSetAndClearSessionCookie(t *testing.T) {
	req := requestWithHeader("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	SetSessionCookie(w, req, "token-123")
	cookie := singleCookie(t, w, SessionCookieName)
	if cookie.Value != "token-123" {
		t.Fatalf("session cookie value = %q, want token-123", cookie.Value)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("session cookie attributes = %+v", cookie)
	}

	w = httptest.NewRecorder()
	ClearSessionCookie(w, req)
	cookie = singleCookie(t, w, SessionCookieName)
	if cookie.Value != "" || cookie.MaxAge != -1 {
		t.Fatalf("cleared session cookie = %+v", cookie)
	}
}

func TestEnsureCSRFCookieReusesExistingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "existing-token"})
	w := httptest.NewRecorder()

	token, err := EnsureCSRFCookie(w, req)
	if err != nil {
		t.Fatalf("EnsureCSRFCookie returned error: %v", err)
	}
	if token != "existing-token" {
		t.Fatalf("token = %q, want existing-token", token)
	}
	if got := w.Result().Cookies(); len(got) != 0 {
		t.Fatalf("EnsureCSRFCookie set cookies = %v, want none", got)
	}
}

func TestEnsureCSRFCookieCreatesToken(t *testing.T) {
	req := requestWithHeader("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	token, err := EnsureCSRFCookie(w, req)
	if err != nil {
		t.Fatalf("EnsureCSRFCookie returned error: %v", err)
	}
	if token == "" {
		t.Fatal("EnsureCSRFCookie returned empty token")
	}
	cookie := singleCookie(t, w, CSRFCookieName)
	if cookie.Value != token {
		t.Fatalf("csrf cookie value = %q, want token %q", cookie.Value, token)
	}
	if cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("csrf cookie attributes = %+v", cookie)
	}
}

func TestClearCSRFCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	ClearCSRFCookie(w, req)
	cookie := singleCookie(t, w, CSRFCookieName)
	if cookie.Value != "" || cookie.MaxAge != -1 || cookie.HttpOnly {
		t.Fatalf("cleared csrf cookie = %+v", cookie)
	}
}

func TestRequireCSRFAllowsSafeMethods(t *testing.T) {
	called := false
	handler := RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("next handler was not called")
	}
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestRequireCSRFRejectsMissingCookie(t *testing.T) {
	w := httptest.NewRecorder()
	RequireCSRF(notCalledHandler(t)).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestRequireCSRFRejectsInvalidToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "cookie-token"})
	req.Header.Set("X-CSRF-Token", "wrong-token")
	w := httptest.NewRecorder()

	RequireCSRF(notCalledHandler(t)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestRequireCSRFAllowsHeaderToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "token"})
	req.Header.Set("X-CSRF-Token", "token")
	w := httptest.NewRecorder()

	RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestRequireCSRFAllowsFormToken(t *testing.T) {
	form := url.Values{"csrf_token": {"token"}}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "token"})
	w := httptest.NewRecorder()

	RequireCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func requestWithHeader(name, value string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(name, value)
	return req
}

func singleCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q was not set", name)
	return nil
}

func notCalledHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not be called")
	})
}

package web

import (
	"html/template"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"sms-gateway/internal/auth"
	"sms-gateway/internal/modem"
	"sms-gateway/internal/queue"
)

// Server holds the web dashboard state and dependencies.
type Server struct {
	templates *template.Template
	auth      *auth.Store
	modem     modem.Modem
	queue     *queue.Queue
	limiter   *loginLimiter
}

type loginAttempt struct {
	count   int
	blocked time.Time
	updated time.Time
}

type loginLimiter struct {
	attempts map[string]loginAttempt
	mu       sync.Mutex
}

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

const (
	maxFormBytes       = 64 << 10
	maxPhoneLength     = 16
	maxMessageLength   = 1600
	maxKeyNameLength   = 100
	maxLoginFailures   = 5
	loginBlockDuration = 15 * time.Minute
	loginAttemptTTL    = 30 * time.Minute
)

// NewServer creates a new web dashboard server.
func NewServer(authStore *auth.Store, m modem.Modem, q *queue.Queue) (*Server, error) {
	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}

	return &Server{
		templates: tmpl,
		auth:      authStore,
		modem:     m,
		queue:     q,
		limiter: &loginLimiter{
			attempts: make(map[string]loginAttempt),
		},
	}, nil
}

// RegisterRoutes registers all dashboard routes on the provided mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Static files (no auth required).
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFileServer()))

	// Login routes (no auth required).
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.Handle("POST /login", auth.RequireCSRF(http.HandlerFunc(s.handleLoginSubmit)))
	mux.Handle("POST /logout", auth.RequireCSRF(http.HandlerFunc(s.handleLogout)))

	// Dashboard routes (admin auth required).
	adminAuth := auth.RequireAdmin(s.auth)
	mux.Handle("GET /", adminAuth(http.HandlerFunc(s.handleRoot)))
	mux.Handle("GET /dashboard", adminAuth(http.HandlerFunc(s.handleDashboard)))
	mux.Handle("GET /dashboard/keys", adminAuth(http.HandlerFunc(s.handleKeysPage)))

	// HTMX partial endpoints (admin auth required).
	mux.Handle("GET /dashboard/partials/status", adminAuth(http.HandlerFunc(s.handlePartialStatus)))
	mux.Handle("GET /dashboard/partials/queue", adminAuth(http.HandlerFunc(s.handlePartialQueue)))
	mux.Handle("GET /dashboard/partials/history", adminAuth(http.HandlerFunc(s.handlePartialHistory)))
	mux.Handle("GET /dashboard/partials/keylist", adminAuth(http.HandlerFunc(s.handlePartialKeyList)))

	// Dashboard actions (admin auth required).
	adminPost := func(handler http.HandlerFunc) http.Handler {
		return adminAuth(auth.RequireCSRF(http.HandlerFunc(handler)))
	}
	mux.Handle("POST /dashboard/send", adminPost(s.handleSendSMS))
	mux.Handle("POST /dashboard/keys", adminPost(s.handleCreateKey))
	mux.Handle("POST /dashboard/keys/revoke", adminPost(s.handleRevokeKey))
}

// render executes a named template with the given data.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	csrfToken, err := auth.EnsureCSRFCookie(w, r)
	if err != nil {
		log.Printf("failed to ensure CSRF cookie: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	data["CSRFToken"] = csrfToken
	data["MessageMaxLength"] = maxMessageLength
	data["PhonePattern"] = `\+[1-9][0-9]{7,14}`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template error (%s): %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// --- Login handlers ---

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to dashboard.
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && s.auth.ValidateSession(cookie.Value) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	data := map[string]any{
		"Error": r.URL.Query().Get("error"),
	}
	s.render(w, r, "login.html", data)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+request", http.StatusSeeOther)
		return
	}

	clientID := clientIdentifier(r)
	if retryAfter, blocked := s.limiter.allow(clientID); blocked {
		http.Redirect(w, r, "/login?error=Too+many+login+attempts.+Try+again+in+"+retryAfter, http.StatusSeeOther)
		return
	}

	password := strings.TrimSpace(r.FormValue("password"))
	if !s.auth.ValidateAdminPassword(password) {
		s.limiter.fail(clientID)
		http.Redirect(w, r, "/login?error=Invalid+password", http.StatusSeeOther)
		return
	}
	s.limiter.reset(clientID)

	token, err := s.auth.CreateSession()
	if err != nil {
		log.Printf("failed to create session: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, r, token)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		s.auth.DestroySession(cookie.Value)
	}
	auth.ClearSessionCookie(w, r)
	auth.ClearCSRFCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Dashboard handlers ---

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	status, statusErr := s.modem.GetStatus()
	queueJobs := s.queue.ListByStatus(queue.StatusQueued, queue.StatusSending)
	historyJobs := s.queue.ListByStatus(queue.StatusSent, queue.StatusFailed)

	// Limit history to last 50 for the initial page load.
	if len(historyJobs) > 50 {
		historyJobs = historyJobs[:50]
	}

	data := map[string]any{
		"Status":    status,
		"StatusErr": "",
		"Queue":     queueJobs,
		"History":   historyJobs,
		"Pending":   s.queue.Pending(),
	}
	if statusErr != nil {
		data["StatusErr"] = statusErr.Error()
	}

	s.render(w, r, "dashboard.html", data)
}

func (s *Server) handleKeysPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": "",
	}
	s.render(w, r, "keys.html", data)
}

// --- HTMX partial handlers ---

func (s *Server) handlePartialStatus(w http.ResponseWriter, r *http.Request) {
	status, statusErr := s.modem.GetStatus()
	data := map[string]any{
		"Status":    status,
		"StatusErr": "",
	}
	if statusErr != nil {
		data["StatusErr"] = statusErr.Error()
	}
	s.render(w, r, "partial_status.html", data)
}

func (s *Server) handlePartialQueue(w http.ResponseWriter, r *http.Request) {
	queueJobs := s.queue.ListByStatus(queue.StatusQueued, queue.StatusSending)
	data := map[string]any{
		"Queue":   queueJobs,
		"Pending": s.queue.Pending(),
	}
	s.render(w, r, "partial_queue.html", data)
}

func (s *Server) handlePartialHistory(w http.ResponseWriter, r *http.Request) {
	historyJobs := s.queue.ListByStatus(queue.StatusSent, queue.StatusFailed)
	if len(historyJobs) > 50 {
		historyJobs = historyJobs[:50]
	}
	data := map[string]any{
		"History": historyJobs,
	}
	s.render(w, r, "partial_history.html", data)
}

func (s *Server) handlePartialKeyList(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": "",
	}
	s.render(w, r, "partial_keylist.html", data)
}

// --- Action handlers ---

func (s *Server) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeNotice(w, http.StatusBadRequest, "error", "Invalid form submission.")
		return
	}

	phone := strings.TrimSpace(r.FormValue("phone"))
	message := strings.TrimSpace(r.FormValue("message"))

	if phone == "" || message == "" {
		s.writeNotice(w, http.StatusBadRequest, "error", "Phone number and message are required.")
		return
	}
	if len(phone) > maxPhoneLength || !phonePattern.MatchString(phone) {
		s.writeNotice(w, http.StatusBadRequest, "error", "Phone number must be in E.164 format, for example +420123456789.")
		return
	}
	if len(message) > maxMessageLength {
		s.writeNotice(w, http.StatusBadRequest, "error", "Message is too long.")
		return
	}

	job, ok := s.queue.Enqueue(phone, message)
	if !ok {
		s.writeNotice(w, http.StatusServiceUnavailable, "error", "Queue is full, try again later.")
		return
	}

	log.Printf("Dashboard: SMS enqueued %s to %s", job.ID, redactPhone(phone))
	s.writeNotice(w, http.StatusAccepted, "success", "SMS enqueued successfully. Job ID: "+job.ID)
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form submission", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Unnamed key"
	}
	if len(name) > maxKeyNameLength {
		http.Error(w, "Key name is too long", http.StatusBadRequest)
		return
	}

	_, rawKey, err := s.auth.CreateAPIKey(name)
	if err != nil {
		log.Printf("failed to create API key: %v", err)
		http.Error(w, "Failed to create API key", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": rawKey,
	}
	s.render(w, r, "partial_keylist.html", data)
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form submission", http.StatusBadRequest)
		return
	}

	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		http.Error(w, "Key ID is required", http.StatusBadRequest)
		return
	}

	if err := s.auth.RevokeAPIKey(id); err != nil {
		log.Printf("failed to revoke API key: %v", err)
		http.Error(w, "Failed to revoke API key", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": "",
	}
	s.render(w, r, "partial_keylist.html", data)
}

func (s *Server) writeNotice(w http.ResponseWriter, status int, noticeType, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<div class="notice ` + noticeType + `" role="alert">` + template.HTMLEscapeString(message) + `</div>`))
}

func clientIdentifier(r *http.Request) string {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func redactPhone(phone string) string {
	if len(phone) <= 4 {
		return phone
	}
	return phone[:3] + strings.Repeat("*", len(phone)-5) + phone[len(phone)-2:]
}

func (l *loginLimiter) allow(clientID string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()

	attempt, ok := l.attempts[clientID]
	if !ok || attempt.blocked.IsZero() || time.Now().After(attempt.blocked) {
		return "", false
	}
	remaining := time.Until(attempt.blocked).Round(time.Second)
	if remaining < time.Second {
		remaining = time.Second
	}
	return remaining.String(), true
}

func (l *loginLimiter) fail(clientID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()

	attempt := l.attempts[clientID]
	attempt.count++
	attempt.updated = time.Now()
	if attempt.count >= maxLoginFailures {
		attempt.blocked = time.Now().Add(loginBlockDuration)
	}
	l.attempts[clientID] = attempt
}

func (l *loginLimiter) reset(clientID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, clientID)
}

func (l *loginLimiter) pruneLocked() {
	now := time.Now()
	for clientID, attempt := range l.attempts {
		if now.Sub(attempt.updated) > loginAttemptTTL && (attempt.blocked.IsZero() || now.After(attempt.blocked)) {
			delete(l.attempts, clientID)
		}
	}
}

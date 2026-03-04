package web

import (
	"html/template"
	"log"
	"net/http"

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
}

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
	}, nil
}

// RegisterRoutes registers all dashboard routes on the provided mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Static files (no auth required).
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFileServer()))

	// Login routes (no auth required).
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)

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
	mux.Handle("POST /dashboard/send", adminAuth(http.HandlerFunc(s.handleSendSMS)))
	mux.Handle("POST /dashboard/keys", adminAuth(http.HandlerFunc(s.handleCreateKey)))
	mux.Handle("POST /dashboard/keys/revoke", adminAuth(http.HandlerFunc(s.handleRevokeKey)))
}

// render executes a named template with the given data.
func (s *Server) render(w http.ResponseWriter, name string, data any) {
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
	s.render(w, "login.html", data)
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	if !s.auth.ValidateAdminPassword(password) {
		http.Redirect(w, r, "/login?error=Invalid+password", http.StatusSeeOther)
		return
	}

	token, err := s.auth.CreateSession()
	if err != nil {
		log.Printf("failed to create session: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, token)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		s.auth.DestroySession(cookie.Value)
	}
	auth.ClearSessionCookie(w)
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

	s.render(w, "dashboard.html", data)
}

func (s *Server) handleKeysPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": "",
	}
	s.render(w, "keys.html", data)
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
	s.render(w, "partial_status.html", data)
}

func (s *Server) handlePartialQueue(w http.ResponseWriter, r *http.Request) {
	queueJobs := s.queue.ListByStatus(queue.StatusQueued, queue.StatusSending)
	data := map[string]any{
		"Queue":   queueJobs,
		"Pending": s.queue.Pending(),
	}
	s.render(w, "partial_queue.html", data)
}

func (s *Server) handlePartialHistory(w http.ResponseWriter, r *http.Request) {
	historyJobs := s.queue.ListByStatus(queue.StatusSent, queue.StatusFailed)
	if len(historyJobs) > 50 {
		historyJobs = historyJobs[:50]
	}
	data := map[string]any{
		"History": historyJobs,
	}
	s.render(w, "partial_history.html", data)
}

func (s *Server) handlePartialKeyList(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": "",
	}
	s.render(w, "partial_keylist.html", data)
}

// --- Action handlers ---

func (s *Server) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	phone := r.FormValue("phone")
	message := r.FormValue("message")

	if phone == "" || message == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="notice error" role="alert">Phone number and message are required.</div>`))
		return
	}

	job, ok := s.queue.Enqueue(phone, message)
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="notice error" role="alert">Queue is full, try again later.</div>`))
		return
	}

	log.Printf("Dashboard: SMS enqueued %s to %s", job.ID, phone)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<div class="notice success" role="alert">SMS enqueued successfully! Job ID: ` + job.ID + `</div>`))
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		name = "Unnamed key"
	}

	apiKey, err := s.auth.CreateAPIKey(name)
	if err != nil {
		log.Printf("failed to create API key: %v", err)
		http.Error(w, "Failed to create API key", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"Keys":   s.auth.ListAPIKeys(),
		"NewKey": apiKey.Key,
	}
	s.render(w, "partial_keylist.html", data)
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
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
	s.render(w, "partial_keylist.html", data)
}

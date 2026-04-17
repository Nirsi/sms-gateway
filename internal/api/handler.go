package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"sms-gateway/internal/modem"
	"sms-gateway/internal/queue"
)

const maxJSONBodyBytes = 64 << 10

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// Handler holds the HTTP handlers for the SMS gateway API.
type Handler struct {
	modem modem.Modem
	queue *queue.Queue
}

// NewHandler creates a new API handler with the given modem and queue instances.
func NewHandler(m modem.Modem, q *queue.Queue) *Handler {
	return &Handler{modem: m, queue: q}
}

// RegisterRoutes registers all API routes on the provided mux.
// If wrap is non-nil, each handler is wrapped with the provided middleware
// (e.g. API key authentication).
func (h *Handler) RegisterRoutes(mux *http.ServeMux, wrap func(http.Handler) http.Handler) {
	register := func(pattern string, handler http.HandlerFunc) {
		if wrap != nil {
			mux.Handle(pattern, wrap(handler))
		} else {
			mux.HandleFunc(pattern, handler)
		}
	}

	register("GET /api/status", h.handleStatus)
	register("POST /api/send", h.handleSendSMS)
	register("GET /api/queue/{id}", h.handleJobStatus)
}

// apiError is a standard error response.
type apiError struct {
	Error string `json:"error"`
}

// sendSMSRequest is the expected JSON body for the send SMS endpoint.
type sendSMSRequest struct {
	Phone   string `json:"phone"`
	Message string `json:"message"`
}

// sendSMSResponse is the response returned when a job is enqueued.
type sendSMSResponse struct {
	ID      string          `json:"id"`
	Status  queue.JobStatus `json:"status"`
	Pending int             `json:"pending"`
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode JSON response: %v", err)
	}
}

// handleStatus returns the current modem status.
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	log.Println("GET /api/status — checking modem status")

	status, err := h.modem.GetStatus(r.Context())
	if err != nil {
		log.Printf("modem status error: %v", err)
		// Still return whatever partial status we gathered along with the error
		if status == nil {
			writeJSON(w, http.StatusServiceUnavailable, apiError{Error: err.Error()})
			return
		}
		// Return partial status with a 200 so the caller can see what we know
		type statusWithError struct {
			*modem.Status
			Error string `json:"error,omitempty"`
		}
		writeJSON(w, http.StatusOK, statusWithError{Status: status, Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// handleSendSMS enqueues an SMS job and returns immediately.
func (h *Handler) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	var req sendSMSRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		log.Printf("POST /api/send — invalid request body: %v", err)
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid request body: " + err.Error()})
		return
	}
	if decoder.More() {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "request body must contain a single JSON object"})
		return
	}

	req.Phone = strings.TrimSpace(req.Phone)
	req.Message = strings.TrimSpace(req.Message)

	if req.Phone == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "phone number is required"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "message is required"})
		return
	}
	if !phonePattern.MatchString(req.Phone) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "phone number must be in E.164 format"})
		return
	}
	if len(req.Message) > 1600 {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "message is too long"})
		return
	}

	job, ok := h.queue.Enqueue(req.Phone, req.Message)
	if !ok {
		log.Printf("POST /api/send — queue full, rejecting request to %s", redactPhone(req.Phone))
		writeJSON(w, http.StatusServiceUnavailable, apiError{Error: "queue is full, try again later"})
		return
	}

	log.Printf("POST /api/send — enqueued job %s to %s", job.ID, redactPhone(req.Phone))
	writeJSON(w, http.StatusAccepted, sendSMSResponse{
		ID:      job.ID,
		Status:  job.Status,
		Pending: h.queue.Pending(),
	})
}

// handleJobStatus returns the current status of a queued SMS job.
func (h *Handler) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "job id is required"})
		return
	}

	job := h.queue.Get(id)
	if job == nil {
		writeJSON(w, http.StatusNotFound, apiError{Error: "job not found"})
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func redactPhone(phone string) string {
	if len(phone) <= 4 {
		return phone
	}
	return fmt.Sprintf("%s***%s", phone[:3], phone[len(phone)-2:])
}

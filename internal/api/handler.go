package api

import (
	"encoding/json"
	"log"
	"net/http"

	"sms-gateway/internal/modem"
	"sms-gateway/internal/queue"
)

// Handler holds the HTTP handlers for the SMS gateway API.
type Handler struct {
	modem *modem.Modem
	queue *queue.Queue
}

// NewHandler creates a new API handler with the given modem and queue instances.
func NewHandler(m *modem.Modem, q *queue.Queue) *Handler {
	return &Handler{modem: m, queue: q}
}

// RegisterRoutes registers all API routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status", h.handleStatus)
	mux.HandleFunc("POST /api/send", h.handleSendSMS)
	mux.HandleFunc("GET /api/queue/{id}", h.handleJobStatus)
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

	status, err := h.modem.GetStatus()
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("POST /api/send — invalid request body: %v", err)
		writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid request body: " + err.Error()})
		return
	}

	if req.Phone == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "phone number is required"})
		return
	}
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "message is required"})
		return
	}

	job, ok := h.queue.Enqueue(req.Phone, req.Message)
	if !ok {
		log.Printf("POST /api/send — queue full, rejecting request to %s", req.Phone)
		writeJSON(w, http.StatusServiceUnavailable, apiError{Error: "queue is full, try again later"})
		return
	}

	log.Printf("POST /api/send — enqueued job %s to %s", job.ID, req.Phone)
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

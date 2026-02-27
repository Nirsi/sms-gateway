package queue

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"sms-gateway/internal/modem"
)

// JobStatus represents the current state of a queued SMS job.
type JobStatus string

const (
	StatusQueued  JobStatus = "queued"
	StatusSending JobStatus = "sending"
	StatusSent    JobStatus = "sent"
	StatusFailed  JobStatus = "failed"
)

// How long completed jobs are kept in the store before cleanup removes them.
const jobRetention = 5 * time.Minute

// How often the cleanup goroutine runs.
const cleanupInterval = 1 * time.Minute

// Job represents a single SMS send request in the queue.
type Job struct {
	ID        string            `json:"id"`
	Phone     string            `json:"phone"`
	Message   string            `json:"message"`
	Status    JobStatus         `json:"status"`
	Result    *modem.SendResult `json:"result,omitempty"`
	Error     string            `json:"error,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Queue manages an in-memory SMS job queue with a single background worker.
type Queue struct {
	jobs  chan *Job
	store map[string]*Job
	mu    sync.RWMutex
	modem modem.Modem
}

// New creates a new Queue and starts the background worker and cleanup goroutine.
// bufferSize controls how many jobs can be waiting before Enqueue rejects new ones.
func New(m modem.Modem, bufferSize int) *Queue {
	q := &Queue{
		jobs:  make(chan *Job, bufferSize),
		store: make(map[string]*Job),
		modem: m,
	}

	go q.worker()
	go q.cleanup()

	return q
}

// Enqueue adds a new SMS job to the queue. Returns the job immediately.
// Returns nil and false if the queue is full.
func (q *Queue) Enqueue(phone, message string) (*Job, bool) {
	job := &Job{
		ID:        generateID(),
		Phone:     phone,
		Message:   message,
		Status:    StatusQueued,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	q.mu.Lock()
	q.store[job.ID] = job
	q.mu.Unlock()

	select {
	case q.jobs <- job:
		log.Printf("job %s queued — to: %s", job.ID, phone)
		return job, true
	default:
		// Channel is full — remove from store and reject.
		q.mu.Lock()
		delete(q.store, job.ID)
		q.mu.Unlock()
		return nil, false
	}
}

// Get returns a job by its ID, or nil if not found.
// Completed jobs (sent or failed) are removed from the store after reading.
func (q *Queue) Get(id string) *Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, ok := q.store[id]
	if !ok {
		return nil
	}

	// Return a copy so the caller can't mutate the stored job.
	cp := *job
	if job.Result != nil {
		resultCopy := *job.Result
		cp.Result = &resultCopy
	}

	// Clean up completed jobs once their status has been read.
	if job.Status == StatusSent || job.Status == StatusFailed {
		delete(q.store, id)
	}

	return &cp
}

// Pending returns the number of jobs waiting in the queue.
func (q *Queue) Pending() int {
	return len(q.jobs)
}

// worker processes jobs from the channel one at a time.
func (q *Queue) worker() {
	for job := range q.jobs {
		q.process(job)
	}
}

// process sends a single SMS and updates the job status.
func (q *Queue) process(job *Job) {
	q.mu.Lock()
	job.Status = StatusSending
	job.UpdatedAt = time.Now()
	q.mu.Unlock()

	log.Printf("job %s sending — to: %s", job.ID, job.Phone)

	result, err := q.modem.SendSMS(job.Phone, job.Message)

	q.mu.Lock()
	defer q.mu.Unlock()

	job.Result = result
	job.UpdatedAt = time.Now()

	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
		log.Printf("job %s failed — %v", job.ID, err)
	} else {
		job.Status = StatusSent
		log.Printf("job %s sent — ref: %s", job.ID, result.MessageReference)
	}
}

// cleanup periodically removes completed jobs that are older than the retention period.
func (q *Queue) cleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-jobRetention)
		removed := 0

		q.mu.Lock()
		for id, job := range q.store {
			if (job.Status == StatusSent || job.Status == StatusFailed) && job.UpdatedAt.Before(cutoff) {
				delete(q.store, id)
				removed++
			}
		}
		q.mu.Unlock()

		if removed > 0 {
			log.Printf("queue cleanup: removed %d completed job(s)", removed)
		}
	}
}

// generateID returns a random 8-byte hex string.
func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

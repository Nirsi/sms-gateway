package queue

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sort"
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
	jobs        chan *Job
	store       map[string]*Job
	mu          sync.RWMutex
	modem       modem.Modem
	historySize int
}

// New creates a new Queue and starts the background worker and cleanup goroutine.
// bufferSize controls how many jobs can be waiting before Enqueue rejects new ones.
// historySize controls how many completed jobs are kept in memory.
func New(m modem.Modem, bufferSize, historySize int) *Queue {
	if historySize < 0 {
		historySize = 0
	}

	q := &Queue{
		jobs:        make(chan *Job, bufferSize),
		store:       make(map[string]*Job),
		modem:       m,
		historySize: historySize,
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
func (q *Queue) Get(id string) *Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	job, ok := q.store[id]
	if !ok {
		return nil
	}

	return q.copyJob(job)
}

// List returns a copy of all jobs, sorted by creation time (newest first).
func (q *Queue) List() []*Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs := make([]*Job, 0, len(q.store))
	for _, job := range q.store {
		jobs = append(jobs, q.copyJob(job))
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	return jobs
}

// ListByStatus returns a copy of jobs filtered by status, sorted newest first.
func (q *Queue) ListByStatus(statuses ...JobStatus) []*Job {
	statusSet := make(map[JobStatus]bool, len(statuses))
	for _, s := range statuses {
		statusSet[s] = true
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	var jobs []*Job
	for _, job := range q.store {
		if statusSet[job.Status] {
			jobs = append(jobs, q.copyJob(job))
		}
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.After(jobs[j].CreatedAt)
	})

	return jobs
}

// Pending returns the number of jobs waiting in the queue.
func (q *Queue) Pending() int {
	return len(q.jobs)
}

// copyJob returns a deep copy of a job so callers cannot mutate store data.
func (q *Queue) copyJob(job *Job) *Job {
	cp := *job
	if job.Result != nil {
		resultCopy := *job.Result
		cp.Result = &resultCopy
	}
	return &cp
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

// cleanup periodically trims completed jobs when the count exceeds historySize.
func (q *Queue) cleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		q.trimHistory()
	}
}

// trimHistory removes the oldest completed jobs if we exceed historySize.
func (q *Queue) trimHistory() {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Collect completed jobs.
	var completed []*Job
	for _, job := range q.store {
		if job.Status == StatusSent || job.Status == StatusFailed {
			completed = append(completed, job)
		}
	}

	if len(completed) <= q.historySize {
		return
	}

	// Sort oldest first.
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].UpdatedAt.Before(completed[j].UpdatedAt)
	})

	// Remove the oldest entries beyond the limit.
	excess := len(completed) - q.historySize
	for i := 0; i < excess; i++ {
		delete(q.store, completed[i].ID)
	}

	log.Printf("queue cleanup: trimmed %d completed job(s) (history limit: %d)", excess, q.historySize)
}

// generateID returns a random 8-byte hex string.
func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

package taskrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"sync"
	"time"

	"workbuddy2api/internal/scheduler"
)

type Runner struct {
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	completionErr error
	store         *Store
	catalog       func(time.Time) []scheduler.TaskInfo
	execute       func(context.Context, string) (scheduler.TaskResult, error)
	active        *Run
	done          chan struct{}
}
type BusyError struct{ RunID string }

func (e *BusyError) Error() string { return "background task busy" }

var ErrDisabled = errors.New("task disabled")
var ErrUnknownTask = errors.New("unknown task")
var ErrRequestConflict = errors.New("request id belongs to another task")

func NewRunner(ctx context.Context, store *Store, catalog func(time.Time) []scheduler.TaskInfo, execute func(context.Context, string) (scheduler.TaskResult, error)) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	return &Runner{ctx: ctx, cancel: cancel, store: store, catalog: catalog, execute: execute}
}
func (r *Runner) checkTask(id string) error {
	if !knownTask(id) {
		return ErrUnknownTask
	}
	for _, task := range r.catalog(time.Now()) {
		if task.ID == id {
			if !task.Enabled {
				return ErrDisabled
			}
			return nil
		}
	}
	return ErrUnknownTask
}
func (r *Runner) StartManual(taskID, requestID string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.store.ByRequest(requestID); ok {
		if old.TaskID != taskID {
			return Run{}, ErrRequestConflict
		}
		return old, nil
	}
	if !requestIDPattern.MatchString(requestID) {
		return Run{}, errors.New("invalid request id")
	}
	if err := r.checkTask(taskID); err != nil {
		return Run{}, err
	}
	if err := r.ctx.Err(); err != nil {
		return Run{}, err
	}
	if r.active != nil {
		return Run{}, &BusyError{RunID: r.active.ID}
	}
	return r.start(taskID, "manual", requestID, nil)
}

// start requires mu; persistence must succeed before any business side effect.
func (r *Runner) start(taskID, source, requestID string, at []time.Time) (Run, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Run{}, err
	}
	run := Run{ID: hex.EncodeToString(raw[:]), TaskID: taskID, Source: source, RequestID: requestID, ScheduledAt: at, StartedAt: time.Now().UTC(), Status: "running"}
	if err := r.store.Put(run, run.StartedAt); err != nil {
		return Run{}, err
	}
	// ponytail: background tasks are globally serial; use per-account scheduling only if throughput requires it
	r.completionErr = nil
	r.active = &run
	r.done = make(chan struct{})
	go r.run(taskID)
	return cloneRun(run), nil
}
func (r *Runner) Scheduled(ctx context.Context, taskID string, at time.Time) {
	for {
		r.mu.Lock()
		if ctx.Err() != nil || r.ctx.Err() != nil {
			r.mu.Unlock()
			return
		}
		if err := r.checkTask(taskID); err != nil {
			r.mu.Unlock()
			log.Print("task_schedule_rejected")
			return
		}
		if r.active != nil {
			done := r.done
			merge := r.active.TaskID == taskID && r.active.Source == "manual"
			if merge {
				updated := cloneRun(*r.active)
				found := false
				for _, old := range updated.ScheduledAt {
					if old.Equal(at) {
						found = true
						break
					}
				}
				if !found {
					updated.ScheduledAt = append(updated.ScheduledAt, at)
				}
				if err := r.store.Put(updated, time.Now()); err != nil {
					log.Printf("task_schedule_merge_write_failed run_id=%s", updated.ID)
				} else {
					r.active = &updated
				}
			}
			r.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				return
			case <-r.ctx.Done():
				return
			}
			if merge {
				return
			}
			continue
		}
		_, err := r.start(taskID, "scheduled", "", []time.Time{at})
		done := r.done
		r.mu.Unlock()
		if err != nil {
			log.Print("task_schedule_start_write_failed")
			return
		}
		select {
		case <-done:
		case <-ctx.Done():
		case <-r.ctx.Done():
		}
		return
	}
}
func (r *Runner) run(taskID string) {
	var result scheduler.TaskResult
	var executeErr error
	panicked := true
	defer func() {
		if panicked {
			_ = recover()
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.active == nil { // Shutdown already persisted the interrupted record.
			close(r.done)
			r.done = nil
			return
		}
		run := cloneRun(*r.active)
		finished := time.Now().UTC()
		if finished.Before(run.StartedAt) {
			finished = run.StartedAt
		}
		duration := finished.Sub(run.StartedAt).Milliseconds()
		run.FinishedAt = &finished
		run.DurationMS = &duration
		run.Accounts = result.Accounts
		run.Log = result.Log
		run.LogTruncated = result.LogTruncated
		run.Status = aggregate(result.Accounts, executeErr)
		if executeErr != nil {
			run.Log = "task_execution_failed\n" + run.Log
		}
		if panicked {
			run.Status = "unknown"
			run.Log = "task_panic: result unknown\n"
		}
		if r.ctx.Err() != nil {
			run.Status = "interrupted"
			run.Log = "task_cancelled: result unknown\n" + run.Log
		}
		r.completionErr = r.store.Put(run, finished)
		if r.completionErr != nil {
			log.Printf("task_completion_write_failed run_id=%s", run.ID)
		}
		r.active = nil
		close(r.done)
		r.done = nil
	}()
	result, executeErr = r.execute(r.ctx, taskID)
	panicked = false
}
func aggregate(accounts []scheduler.AccountResult, executeErr error) string {
	success, failed, unknown := false, executeErr != nil, false
	for _, account := range accounts {
		switch account.Status {
		case "success":
			success = true
		case "failed":
			failed = true
		case "skipped":
		default:
			unknown = true
		}
	}
	switch {
	case failed && (success || unknown):
		return "partial_failure"
	case failed:
		return "failed"
	case unknown:
		return "unknown"
	case success:
		return "success"
	default:
		return "skipped"
	}
}
func (r *Runner) Active() *Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return nil
	}
	copy := cloneRun(*r.active)
	return &copy
}

// Shutdown cancels dispatch and waits for a terminal record. Some legacy RPCs
// ignore cancellation, so at the deadline we persist an interrupted result
// before the process exits; a late RPC must not overwrite that result.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.cancel()
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.active == nil {
				return r.completionErr
			}
			run := cloneRun(*r.active)
			now := time.Now().UTC()
			if now.Before(run.StartedAt) {
				now = run.StartedAt
			}
			run.Status, run.FinishedAt, run.DurationMS = "interrupted", &now, nil
			run.Log = "shutdown_deadline: business finish time and result unknown\n" + run.Log
			r.completionErr = r.store.Put(run, now)
			r.active = nil
			return errors.Join(ctx.Err(), r.completionErr)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completionErr
}

package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
	"workbuddy2api/internal/scheduler"
)

func TestShutdownWaitsForInterruptedHistory(t *testing.T) {
	r, s, _ := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		<-ctx.Done()
		return scheduler.TaskResult{}, ctx.Err()
	})
	run, err := r.StartManual("checkin", "shutdown-request-123")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	assertInterruptedOnDisk(t, s, run.ID)
	if _, err := r.StartManual("checkin", "shutdown-request-456"); !errors.Is(err, context.Canceled) {
		t.Fatalf("accepted task after shutdown: %v", err)
	}
}

func TestShutdownBoundsNonCooperativeTaskAndDoesNotOverwrite(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	s := openTestStore(t)
	r := NewRunner(context.Background(), s, func(time.Time) []scheduler.TaskInfo { return []scheduler.TaskInfo{{ID: "checkin", Enabled: true}} }, func(context.Context, string) (scheduler.TaskResult, error) {
		<-release
		return scheduler.TaskResult{Accounts: []scheduler.AccountResult{{UID: "test", Status: "success"}}}, nil
	})
	run, err := r.StartManual("checkin", "shutdown-request-123")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error: %v", err)
	}
	assertInterruptedOnDisk(t, s, run.ID)
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("execution did not finish")
	}
	assertInterruptedOnDisk(t, s, run.ID)
}

func assertInterruptedOnDisk(t *testing.T, s *Store, id string) {
	t.Helper()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	var h history
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Runs) != 1 || h.Runs[0].ID != id || h.Runs[0].Status != "interrupted" || h.Runs[0].FinishedAt == nil {
		t.Fatalf("not durably interrupted: %+v", h.Runs)
	}
}

func TestShutdownReturnsPersistenceFailure(t *testing.T) {
	r, s, _ := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		<-ctx.Done()
		return scheduler.TaskResult{}, ctx.Err()
	})
	original := s.write
	failed := errors.New("disk failure")
	writes := 0
	s.write = func(path string, data []byte) error {
		writes++
		if writes > 1 {
			return failed
		}
		return original(path, data)
	}
	if _, err := r.StartManual("checkin", "shutdown-request-123"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Shutdown(ctx); !errors.Is(err, failed) {
		t.Fatalf("lost persistence failure: %v", err)
	}
}

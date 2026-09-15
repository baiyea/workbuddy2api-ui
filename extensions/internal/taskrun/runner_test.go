package taskrun

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"workbuddy2api/internal/scheduler"
)

func testRunner(t *testing.T, execute func(context.Context, string) (scheduler.TaskResult, error)) (*Runner, *Store, context.CancelFunc) {
	t.Helper()
	s := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	r := NewRunner(ctx, s, func(time.Time) []scheduler.TaskInfo {
		return []scheduler.TaskInfo{{ID: "checkin", Enabled: true}, {ID: "travel", Enabled: true}, {ID: "cat", Enabled: false}}
	}, execute)
	t.Cleanup(func() {
		cancel()
		r.mu.Lock()
		done := r.done
		r.mu.Unlock()
		if done != nil {
			<-done
		}
	})
	return r, s, cancel
}
func waitRun(t *testing.T, r *Runner, id string) Run {
	t.Helper()
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
	got, ok := r.store.Get(id)
	if !ok {
		t.Fatal("run missing")
	}
	return got
}
func TestManualIdempotencyBeforeBusyAndDisabled(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	r, _, _ := testRunner(t, func(ctx context.Context, id string) (scheduler.TaskResult, error) {
		count.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return scheduler.TaskResult{}, nil
	})
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	same, err := r.StartManual("checkin", "request-1234567890")
	if err != nil || same.ID != first.ID {
		t.Fatal("retry duplicated")
	}
	if _, err := r.StartManual("travel", "request-1234567890"); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("conflict lost: %v", err)
	}
	if _, err := r.StartManual("cat", "request-1234567891"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled allowed: %v", err)
	}
	if _, err := r.StartManual("shell", "request-1234567891"); !errors.Is(err, ErrUnknownTask) {
		t.Fatal("unknown allowed")
	}
	_, err = r.StartManual("travel", "request-1234567891")
	var busy *BusyError
	if !errors.As(err, &busy) || busy.RunID != first.ID {
		t.Fatalf("busy=%v", err)
	}
	active := r.Active()
	active.ID = "mutated"
	if r.Active().ID != first.ID {
		t.Fatal("Active leaked pointer")
	}
	close(release)
	got := waitRun(t, r, first.ID)
	if got.Status != "skipped" || r.Active() != nil || count.Load() != 1 {
		t.Fatal("execution/slot not finalized")
	}
	r.catalog = func(time.Time) []scheduler.TaskInfo { return nil }
	same, err = r.StartManual("checkin", "request-1234567890")
	if err != nil || same.ID != first.ID || same.Status != "skipped" {
		t.Fatal("completed retry checked catalog first")
	}
}
func TestConcurrentManualStartsExecuteOnce(t *testing.T) {
	entered := make(chan struct{})
	var count atomic.Int32
	r, _, cancel := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		count.Add(1)
		close(entered)
		<-ctx.Done()
		return scheduler.TaskResult{}, ctx.Err()
	})
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := r.StartManual("checkin", "concurrent-request-1")
			ids <- got.ID
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	<-entered
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("multiple run IDs")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	got := waitRun(t, r, first)
	if got.Status != "interrupted" || count.Load() != 1 {
		t.Fatal("cancel/idempotency failed")
	}
}
func TestManualRejectsInvalidRequestAndCancelledLifecycle(t *testing.T) {
	r, _, cancel := testRunner(t, func(context.Context, string) (scheduler.TaskResult, error) {
		t.Error("unexpected execute")
		return scheduler.TaskResult{}, nil
	})
	for _, id := range []string{"", "short", strings.Repeat("a", 81), "request-你好-123456789"} {
		if _, err := r.StartManual("checkin", id); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	cancel()
	if _, err := r.StartManual("checkin", "request-1234567890"); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown accepted: %v", err)
	}
}
func TestScheduledMergesManualTriggerAndWaitsForDone(t *testing.T) {
	entered, release, merged := make(chan struct{}), make(chan struct{}), make(chan struct{}, 2)
	r, s, cancel := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return scheduler.TaskResult{}, nil
	})
	originalWrite := s.write
	s.write = func(path string, b []byte) error {
		err := originalWrite(path, b)
		if strings.Contains(string(b), `"scheduled_at":["`) {
			select {
			case merged <- struct{}{}:
			default:
			}
		}
		return err
	}
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	at := time.Now().UTC()
	done := make(chan struct{})
	go func() { r.Scheduled(r.ctx, "checkin", at); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	<-merged
	select {
	case <-done:
		t.Fatal("merged schedule returned before execution finished")
	default:
	}
	dup := make(chan struct{})
	go func() { r.Scheduled(r.ctx, "checkin", at); close(dup) }()
	t.Cleanup(func() { cancel(); <-dup })
	<-merged
	at2 := at.Add(time.Second)
	done2 := make(chan struct{})
	go func() { r.Scheduled(r.ctx, "checkin", at2); close(done2) }()
	t.Cleanup(func() { cancel(); <-done2 })
	<-merged
	close(release)
	<-done
	<-done2
	<-dup
	got := waitRun(t, r, first.ID)
	if len(got.ScheduledAt) != 2 || got.Source != "manual" || got.Status != "skipped" {
		t.Fatalf("merge overwritten: %+v", got)
	}
}
func TestScheduledDifferentTasksWaitAndPreserveCallerOrder(t *testing.T) {
	entered := make(chan string, 3)
	release := make(chan struct{})
	var count atomic.Int32
	r, s, cancel := testRunner(t, func(ctx context.Context, id string) (scheduler.TaskResult, error) {
		entered <- id
		if count.Add(1) == 1 {
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return scheduler.TaskResult{}, nil
	})
	checked := make(chan struct{}, 2)
	r.catalog = func(time.Time) []scheduler.TaskInfo {
		select {
		case checked <- struct{}{}:
		default:
		}
		return []scheduler.TaskInfo{{ID: "checkin", Enabled: true}, {ID: "travel", Enabled: true}}
	}
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if <-entered != "checkin" {
		t.Fatal("wrong first task")
	}
	<-checked
	done := make(chan struct{})
	go func() {
		defer close(done)
		at := time.Now()
		r.Scheduled(r.ctx, "travel", at)
		r.Scheduled(r.ctx, "checkin", at)
	}()
	t.Cleanup(func() { cancel(); <-done })
	<-checked // Scheduled has reached the busy path before the active task can finish.
	select {
	case id := <-entered:
		t.Fatalf("overlapping execution: %s", id)
	default:
	}
	close(release)
	<-done
	if <-entered != "travel" || <-entered != "checkin" || count.Load() != 3 {
		t.Fatal("scheduled order changed")
	}
	rows, _, _ := s.Page("", 10)
	if len(rows) != 3 || rows[2].ID != first.ID || rows[1].Source != "scheduled" || len(rows[0].ScheduledAt) != 1 {
		t.Fatalf("history=%+v", rows)
	}
}

func TestWaitingScheduleCancellationNeverRunsLater(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	r, _, cancel := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return scheduler.TaskResult{}, ctx.Err()
	})
	checked := make(chan struct{}, 2)
	r.catalog = func(time.Time) []scheduler.TaskInfo {
		checked <- struct{}{}
		return []scheduler.TaskInfo{{ID: "checkin", Enabled: true}, {ID: "travel", Enabled: true}}
	}
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	<-checked
	scheduleCtx, stopSchedule := context.WithCancel(r.ctx)
	done := make(chan struct{})
	go func() { r.Scheduled(scheduleCtx, "travel", time.Now()); close(done) }()
	t.Cleanup(func() { stopSchedule(); cancel(); <-done })
	<-checked
	stopSchedule()
	<-done
	cancel()
	waitRun(t, r, first.ID)
	if calls.Load() != 1 {
		t.Fatal("cancelled schedule executed")
	}
}

func TestBalancesDoNotBecomeRewards(t *testing.T) {
	now := time.Now()
	reward := int64(3)
	r, _, _ := testRunner(t, func(context.Context, string) (scheduler.TaskResult, error) {
		return scheduler.TaskResult{Accounts: []scheduler.AccountResult{
			{UID: "observed", Status: "success", Before: &scheduler.Balance{Value: 10, ObservedAt: now}, After: &scheduler.Balance{Value: 25, ObservedAt: now}},
			{UID: "confirmed", Status: "success", Reward: &reward},
		}}, nil
	})
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	got := waitRun(t, r, first.ID)
	if got.Accounts[0].Reward != nil || got.Accounts[0].Before.Value != 10 || got.Accounts[0].After.Value != 25 || *got.Accounts[1].Reward != 3 || got.Accounts[1].Before != nil || got.Accounts[1].After != nil {
		t.Fatalf("reward inferred or observation lost: %+v", got.Accounts)
	}
}
func TestStartPersistenceFailureNeverExecutes(t *testing.T) {
	var calls atomic.Int32
	r, s, _ := testRunner(t, func(context.Context, string) (scheduler.TaskResult, error) {
		calls.Add(1)
		return scheduler.TaskResult{}, nil
	})
	s.write = func(string, []byte) error { return errors.New("injected secret disk path") }
	if _, err := r.StartManual("checkin", "request-1234567890"); err == nil {
		t.Fatal("write failure hidden")
	}
	r.Scheduled(r.ctx, "checkin", time.Now())
	if calls.Load() != 0 || r.Active() != nil {
		t.Fatal("unrecorded execution")
	}
}
func TestCompletionPersistenceFailureReleasesSlotWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	r, s, _ := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return scheduler.TaskResult{Accounts: []scheduler.AccountResult{{UID: "one", Status: "success"}}}, nil
	})
	originalWrite := s.write
	var writes atomic.Int32
	s.write = func(path string, b []byte) error {
		if writes.Add(1) > 1 {
			return errors.New("injected failure")
		}
		return originalWrite(path, b)
	}
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	close(release)
	got := waitRun(t, r, first.ID)
	if got.Status != "running" || r.Active() != nil {
		t.Fatal("unpersisted completion published or lock leaked")
	}
	same, err := r.StartManual("checkin", "request-1234567890")
	if err != nil || same.ID != first.ID || same.Status != "running" || calls.Load() != 1 {
		t.Fatal("failed save caused replay")
	}
	reopened, err := OpenStore(s.path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ = reopened.Get(first.ID)
	if got.Status != "interrupted" {
		t.Fatal("disk falsely completed")
	}
}
func TestScheduledMergeFailureDoesNotReplayManual(t *testing.T) {
	entered, release, attempted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r, s, cancel := testRunner(t, func(ctx context.Context, _ string) (scheduler.TaskResult, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return scheduler.TaskResult{}, nil
	})
	original := s.write
	s.write = func(path string, b []byte) error {
		if strings.Contains(string(b), `"scheduled_at":["`) {
			close(attempted)
			return errors.New("merge failed")
		}
		return original(path, b)
	}
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	done := make(chan struct{})
	go func() { r.Scheduled(r.ctx, "checkin", time.Now()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	<-attempted
	close(release)
	<-done
	got := waitRun(t, r, first.ID)
	if len(got.ScheduledAt) != 0 || got.Status != "skipped" || calls.Load() != 1 {
		t.Fatal("failed merge replayed or published")
	}
}
func TestResultAggregation(t *testing.T) {
	for _, tc := range []struct {
		name           string
		statuses       []string
		executionError bool
		want           string
	}{
		{"empty", nil, false, "skipped"}, {"skip", []string{"skipped"}, false, "skipped"}, {"success", []string{"success", "skipped"}, false, "success"},
		{"unknown", []string{"success", "unknown", "skipped"}, false, "unknown"}, {"failed", []string{"failed", "skipped"}, false, "failed"},
		{"partial success", []string{"failed", "success"}, false, "partial_failure"}, {"partial unknown", []string{"failed", "unknown"}, false, "partial_failure"},
		{"error no accounts", nil, true, "failed"}, {"error skip", []string{"skipped"}, true, "failed"}, {"error all success", []string{"success"}, true, "partial_failure"},
		{"error unknown", []string{"unknown"}, true, "partial_failure"}, {"error failed", []string{"failed"}, true, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _ := testRunner(t, func(context.Context, string) (scheduler.TaskResult, error) {
				out := scheduler.TaskResult{}
				for i, status := range tc.statuses {
					out.Accounts = append(out.Accounts, scheduler.AccountResult{UID: string(rune('a' + i)), Status: status})
				}
				var err error
				if tc.executionError {
					err = errors.New("sensitive upstream response")
				}
				return out, err
			})
			first, err := r.StartManual("checkin", "request-1234567890")
			if err != nil {
				t.Fatal(err)
			}
			got := waitRun(t, r, first.ID)
			if got.Status != tc.want || len(got.Accounts) != len(tc.statuses) || strings.Contains(got.Log, "sensitive") {
				t.Fatalf("result=%+v", got)
			}
			if got.FinishedAt == nil || got.DurationMS == nil {
				t.Fatal("missing completion timing")
			}
		})
	}
}
func TestPanicBecomesUnknownAndReleasesSlot(t *testing.T) {
	r, s, _ := testRunner(t, func(context.Context, string) (scheduler.TaskResult, error) { panic("secret payload") })
	first, err := r.StartManual("checkin", "request-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	got := waitRun(t, r, first.ID)
	if got.Status != "unknown" || !strings.Contains(got.Log, "panic") || r.Active() != nil {
		t.Fatalf("panic leaked slot: %+v", got)
	}
	raw, _ := os.ReadFile(s.path)
	if strings.Contains(string(raw), "secret payload") {
		t.Fatal("panic secret persisted")
	}
}

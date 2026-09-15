package taskrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"workbuddy2api/internal/scheduler"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "tasks", "runs.json"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func runningRun(id string, now time.Time) Run {
	return Run{ID: id, TaskID: "checkin", Source: "manual", RequestID: "request-1234567890-" + id, StartedAt: now, Status: "running"}
}
func completedRun(id string, now time.Time) Run {
	r := runningRun(id, now)
	r.Status = "success"
	finish := now.Add(time.Second)
	duration := int64(1000)
	r.FinishedAt = &finish
	r.DurationMS = &duration
	return r
}
func TestRestartMarksRunningInterrupted(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	r := runningRun("run-1", now)
	if err := s.Put(r, now); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(s.path, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.Get(r.ID)
	if !ok || got.Status != "interrupted" || got.DurationMS != nil || got.FinishedAt == nil || !got.FinishedAt.Equal(now.Add(time.Minute)) || !strings.Contains(got.Log, "unknown") {
		t.Fatalf("false certainty after restart: %+v", got)
	}
	again, err := OpenStore(s.path, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := again.Get(r.ID)
	if !persisted.FinishedAt.Equal(*got.FinishedAt) {
		t.Fatal("recovery was not persisted")
	}
}
func TestStoreFailedWriteDoesNotPublishOrPrune(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	old := runningRun("old", now.Add(-31*24*time.Hour))
	if err := s.Put(old, now); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.write = func(string, []byte) error { return errors.New("injected write failure") }
	if err := s.Put(completedRun("new", now), now); err == nil {
		t.Fatal("write error lost")
	}
	if _, ok := s.Get("new"); ok {
		t.Fatal("unpersisted run published")
	}
	if _, ok := s.Get("old"); !ok {
		t.Fatal("active run pruned")
	}
	after, _ := os.ReadFile(s.path)
	if string(before) != string(after) {
		t.Fatal("failed writer changed disk")
	}
}

func TestStoreDoesNotRetainNewExpiredTerminal(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	if err := s.Put(completedRun("expired", now.Add(-31*24*time.Hour)), now); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("expired"); ok {
		t.Fatal("expired terminal added to recent history")
	}
}

func TestStoreRefusesToEvictOnlyActiveRecords(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	// Private seeding avoids 1000 fsyncs; Put still runs the real validation/pruning/writer.
	for i := 0; i < 1000; i++ {
		s.runs = append(s.runs, runningRun(fmt.Sprintf("active-%d", i), now))
	}
	if err := s.Put(runningRun("overflow", now), now); err == nil {
		t.Fatal("active overflow accepted")
	}
	if len(s.runs) != 1000 {
		t.Fatal("active history evicted")
	}
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("rejected overflow wrote history")
	}
}
func TestStoreDeepCopiesAndExplicitNulls(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	r := runningRun("a", now)
	reward := int64(7)
	r.ScheduledAt = []time.Time{now}
	r.Accounts = []scheduler.AccountResult{{UID: "uid", Status: "success", Before: &scheduler.Balance{Value: 10, ObservedAt: now}, Reward: &reward}}
	if err := s.Put(r, now); err != nil {
		t.Fatal(err)
	}
	r.Accounts[0].Before.Value = 99
	r.ScheduledAt[0] = time.Time{}
	reward = 99
	got, _ := s.Get("a")
	if got.Accounts[0].Before.Value != 10 || *got.Accounts[0].Reward != 7 || got.ScheduledAt[0].IsZero() {
		t.Fatal("Put aliases input")
	}
	got.Accounts[0].Before.Value = 88
	*got.Accounts[0].Reward = 88
	retry, _ := s.ByRequest(r.RequestID)
	retry.Accounts[0].Before.Value = 77
	page, _, err := s.Page("", 20)
	if err != nil {
		t.Fatal(err)
	}
	page[0].Accounts[0].Before.Value = 66
	fresh, _ := s.Get("a")
	if fresh.Accounts[0].Before.Value != 10 || *fresh.Accounts[0].Reward != 7 {
		t.Fatal("read aliases store")
	}
	raw, _ := json.Marshal(fresh)
	for _, want := range []string{`"after":null`, `"finished_at":null`, `"duration_ms":null`} {
		if !strings.Contains(string(raw), want) {
			t.Fatal(string(raw))
		}
	}
}
func TestStoreRetentionPaginationAndUTF8(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	active := runningRun("active", now.Add(-40*24*time.Hour))
	if err := s.Put(active, now); err != nil {
		t.Fatal(err)
	}
	// Seed a valid on-disk fixture to exercise the production pruning once, not 1000 fsyncs.
	rows := []Run{active, completedRun("expired", now.Add(-31*24*time.Hour))}
	for i := 0; i < 998; i++ {
		rows = append(rows, completedRun(fmt.Sprintf("r-%d", i), now.Add(-time.Hour)))
	}
	raw, _ := json.Marshal(struct {
		Version int   `json:"version"`
		Runs    []Run `json:"runs"`
	}{1, rows})
	if err := os.WriteFile(s.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Loading recovery is covered separately; keep active in memory for retention assertions.
	s.runs = rows
	newest := completedRun("new", now)
	newest.Log = strings.Repeat("中", 22000)
	if err := s.Put(newest, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("expired"); ok {
		t.Fatal("expired terminal retained")
	}
	if _, ok := s.Get("active"); !ok {
		t.Fatal("active pruned")
	}
	if err := s.Put(completedRun("newer", now), now); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("r-0"); ok {
		t.Fatal("oldest terminal beyond 1000 retained")
	}
	page, next, err := s.Page("", 2)
	if err != nil || len(page) != 2 || page[0].ID != "newer" || page[1].ID != "new" || next != "new" {
		t.Fatalf("page=%+v cursor=%s err=%v", page, next, err)
	}
	page, next, err = s.Page(next, 100)
	if err != nil || len(page) != 100 || page[0].ID != "r-997" || next != "r-898" {
		t.Fatal("cursor did not advance")
	}
	for _, limit := range []int{0, 101} {
		if _, _, err := s.Page("", limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, _, err := s.Page("../../missing", 20); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	got, _ := s.Get("new")
	if len(got.Log) > 65536 || !utf8.ValidString(got.Log) || !got.LogTruncated {
		t.Fatal("unsafe log truncation")
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("history permissions")
	}
}
func TestStoreRejectsCorruptHistoryWithoutOverwrite(t *testing.T) {
	now := time.Now().UTC()
	base := completedRun("one", now)
	tests := map[string][]byte{"malformed": []byte(`{broken`), "version": []byte(`{"version":2,"runs":[]}`), "unknown field": []byte(`{"version":1,"runs":[],"extra":1}`), "null runs": []byte(`{"version":1,"runs":null}`), "trailing": []byte(`{"version":1,"runs":[]} {}`)}
	for _, name := range []string{"duplicate id", "duplicate request", "invalid task", "status", "zero time", "finish before start", "negative duration", "bad account", "too many accounts", "too many triggers", "too many runs"} {
		rows := []Run{base}
		switch name {
		case "duplicate id":
			rows = append(rows, base)
		case "duplicate request":
			b := base
			b.ID = "two"
			rows = append(rows, b)
		case "invalid task":
			rows[0].TaskID = "shell"
		case "status":
			rows[0].Status = "ok"
		case "zero time":
			rows[0].StartedAt = time.Time{}
		case "finish before start":
			v := now.Add(-time.Second)
			rows[0].FinishedAt = &v
		case "negative duration":
			v := int64(-1)
			rows[0].DurationMS = &v
		case "bad account":
			rows[0].Accounts = []scheduler.AccountResult{{UID: "uid", Status: "ok"}}
		case "too many accounts":
			rows[0].Accounts = make([]scheduler.AccountResult, 10001)
		case "too many triggers":
			rows[0].ScheduledAt = make([]time.Time, 10001)
		case "too many runs":
			rows = make([]Run, 1001)
		}
		raw, _ := json.Marshal(struct {
			Version int   `json:"version"`
			Runs    []Run `json:"runs"`
		}{1, rows})
		tests[name] = raw
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runs.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenStore(path, now); err == nil {
				t.Fatal("corrupt file accepted")
			}
			got, _ := os.ReadFile(path)
			if string(got) != string(raw) {
				t.Fatal("corrupt file overwritten")
			}
		})
	}
}

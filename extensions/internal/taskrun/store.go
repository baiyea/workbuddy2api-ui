// Package taskrun owns bounded history and the sole background-task runner.
package taskrun

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"workbuddy2api/internal/scheduler"
)

type Run struct {
	ID           string                    `json:"id"`
	TaskID       string                    `json:"task_id"`
	Source       string                    `json:"source"`
	RequestID    string                    `json:"request_id"`
	ScheduledAt  []time.Time               `json:"scheduled_at"`
	StartedAt    time.Time                 `json:"started_at"`
	FinishedAt   *time.Time                `json:"finished_at"`
	DurationMS   *int64                    `json:"duration_ms"`
	Status       string                    `json:"status"`
	Accounts     []scheduler.AccountResult `json:"accounts"`
	Log          string                    `json:"log"`
	LogTruncated bool                      `json:"log_truncated"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	runs  []Run
	write func(string, []byte) error
}
type history struct {
	Version int   `json:"version"`
	Runs    []Run `json:"runs"`
}

const maxRuns = 1000
const maxItems = 10000
const maxLog = 65536
const maxFile = 128 << 20

var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

func OpenStore(path string, now time.Time) (*Store, error) {
	s := &Store{path: path, runs: []Run{}, write: writeHistory}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFile {
		return nil, errors.New("invalid task history file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxFile+1))
	dec.DisallowUnknownFields()
	var h history
	if err := dec.Decode(&h); err != nil {
		return nil, errors.New("invalid task history JSON")
	}
	if dec.Decode(new(any)) != io.EOF || h.Version != 1 || h.Runs == nil || len(h.Runs) > maxRuns {
		return nil, errors.New("invalid task history format")
	}
	ids, requests := map[string]bool{}, map[string]bool{}
	for _, r := range h.Runs {
		if err := validateRun(r); err != nil {
			return nil, err
		}
		if ids[r.ID] || (r.RequestID != "" && requests[r.RequestID]) {
			return nil, errors.New("duplicate task history identity")
		}
		ids[r.ID] = true
		if r.RequestID != "" {
			requests[r.RequestID] = true
		}
	}
	s.runs = h.Runs
	for _, r := range h.Runs {
		if r.Status == "running" {
			observed := now
			if observed.Before(r.StartedAt) {
				observed = r.StartedAt
			}
			r.Status = "interrupted"
			r.FinishedAt = &observed
			r.DurationMS = nil
			r.Log = "restart_observed: business finish time and result unknown\n" + r.Log
			if err := s.Put(r, now); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

func knownTask(id string) bool {
	switch id {
	case "checkin", "travel", "activity", "keepalive", "school", "cat":
		return true
	}
	return false
}
func validateRun(r Run) error {
	invalid := errors.New("invalid task history record")
	if !runIDPattern.MatchString(r.ID) || !knownTask(r.TaskID) || r.StartedAt.IsZero() || len(r.Accounts) > maxItems || len(r.ScheduledAt) > maxItems || len(r.Log) > maxLog || !utf8.ValidString(r.Log) {
		return invalid
	}
	if (r.Source != "manual" && r.Source != "scheduled") || (r.Source == "manual" && !requestIDPattern.MatchString(r.RequestID)) || (r.Source == "scheduled" && (r.RequestID != "" || len(r.ScheduledAt) == 0)) {
		return invalid
	}
	switch r.Status {
	case "running":
		if r.FinishedAt != nil || r.DurationMS != nil {
			return invalid
		}
	case "success", "partial_failure", "failed", "skipped", "interrupted", "unknown":
		if r.FinishedAt == nil {
			return invalid
		}
	default:
		return invalid
	}
	if r.FinishedAt != nil && (r.FinishedAt.IsZero() || r.FinishedAt.Before(r.StartedAt)) {
		return invalid
	}
	if r.DurationMS != nil && *r.DurationMS < 0 {
		return invalid
	}
	seenTimes := map[time.Time]bool{}
	for _, at := range r.ScheduledAt {
		key := at.UTC()
		if at.IsZero() || seenTimes[key] {
			return invalid
		}
		seenTimes[key] = true
	}
	seenUID := map[string]bool{}
	for _, a := range r.Accounts {
		if a.UID == "" || len(a.UID) > 256 || len(a.Detail) > 4096 || !utf8.ValidString(a.UID) || !utf8.ValidString(a.Detail) || seenUID[a.UID] {
			return invalid
		}
		seenUID[a.UID] = true
		switch a.Status {
		case "success", "failed", "skipped", "unknown":
		default:
			return invalid
		}
		for _, b := range []*scheduler.Balance{a.Before, a.After} {
			if b != nil && b.ObservedAt.IsZero() {
				return invalid
			}
		}
		if a.Reward != nil && *a.Reward < 0 {
			return invalid
		}
	}
	return nil
}
func cloneRun(r Run) Run {
	r.ScheduledAt = append([]time.Time{}, r.ScheduledAt...)
	r.Accounts = append([]scheduler.AccountResult{}, r.Accounts...)
	if r.FinishedAt != nil {
		v := *r.FinishedAt
		r.FinishedAt = &v
	}
	if r.DurationMS != nil {
		v := *r.DurationMS
		r.DurationMS = &v
	}
	for i := range r.Accounts {
		a := &r.Accounts[i]
		if a.Before != nil {
			v := *a.Before
			a.Before = &v
		}
		if a.After != nil {
			v := *a.After
			a.After = &v
		}
		if a.Reward != nil {
			v := *a.Reward
			a.Reward = &v
		}
	}
	return r
}
func (s *Store) Put(run Run, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run = cloneRun(run)
	clean := strings.ToValidUTF8(run.Log, "�")
	if clean != run.Log {
		run.LogTruncated = true
	}
	run.Log = clean
	if len(run.Log) > maxLog {
		run.Log = run.Log[:maxLog]
		for !utf8.ValidString(run.Log) {
			run.Log = run.Log[:len(run.Log)-1]
		}
		run.LogTruncated = true
	}
	if err := validateRun(run); err != nil {
		return err
	}
	next := make([]Run, 0, len(s.runs)+1)
	found := false
	for _, old := range s.runs {
		if old.ID == run.ID {
			old = run
			found = true
		} else if run.RequestID != "" && old.RequestID == run.RequestID {
			return errors.New("duplicate request identity")
		}
		if old.Status != "running" && old.StartedAt.Before(now.Add(-30*24*time.Hour)) {
			continue
		}
		next = append(next, cloneRun(old))
	}
	if !found && (run.Status == "running" || !run.StartedAt.Before(now.Add(-30*24*time.Hour))) {
		next = append(next, run)
	}
	// Creation order is preserved; only terminal history may be evicted.
	for i := 0; len(next) > maxRuns && i < len(next); {
		if next[i].Status != "running" {
			next = append(next[:i], next[i+1:]...)
		} else {
			i++
		}
	}
	if len(next) > maxRuns {
		return errors.New("task history active limit reached")
	}
	data, err := json.Marshal(history{1, next})
	if err != nil {
		return err
	}
	if len(data) > maxFile {
		return errors.New("task history file limit reached")
	}
	if err := s.write(s.path, append(data, '\n')); err != nil {
		return err
	}
	s.runs = next
	return nil
}
func writeHistory(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".runs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = io.Copy(f, bytes.NewReader(data))
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) Get(id string) (Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.ID == id {
			return cloneRun(r), true
		}
	}
	return Run{}, false
}
func (s *Store) ByRequest(id string) (Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" {
		for _, r := range s.runs {
			if r.RequestID == id {
				return cloneRun(r), true
			}
		}
	}
	return Run{}, false
}
func (s *Store) Page(before string, limit int) ([]Run, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 100 {
		return nil, "", errors.New("invalid page limit")
	}
	end := len(s.runs)
	if before != "" {
		end = -1
		for i := range s.runs {
			if s.runs[i].ID == before {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, "", errors.New("invalid history cursor")
		}
	}
	out := []Run{}
	i := end - 1
	for ; i >= 0 && len(out) < limit; i-- {
		out = append(out, cloneRun(s.runs[i]))
	}
	next := ""
	if i >= 0 {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

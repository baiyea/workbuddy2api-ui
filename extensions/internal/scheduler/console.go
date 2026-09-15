package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"workbuddy2api/internal/auth"
)

type TaskInfo struct {
	ID       string     `json:"id"`
	Enabled  bool       `json:"enabled"`
	Hours    []int      `json:"hours"`
	Timezone string     `json:"timezone"`
	NextAt   *time.Time `json:"next_at"`
}
type Balance struct {
	Value      int64     `json:"value"`
	ObservedAt time.Time `json:"observed_at"`
}
type AccountResult struct {
	UID    string   `json:"uid"`
	Status string   `json:"status"`
	Detail string   `json:"detail"`
	Before *Balance `json:"before"`
	After  *Balance `json:"after"`
	Reward *int64   `json:"reward"`
}
type TaskResult struct {
	Accounts     []AccountResult `json:"accounts"`
	Log          string          `json:"log"`
	LogTruncated bool            `json:"log_truncated"`
}

func (s *Scheduler) taskID(k taskKind) string {
	switch k {
	case taskCheckin:
		return "checkin"
	case taskTravel:
		return "travel"
	case taskActivity:
		return "activity"
	case taskKeepalive:
		return "keepalive"
	case taskSchool:
		return "school"
	case taskCat:
		return "cat"
	}
	return ""
}

func (s *Scheduler) TaskCatalog(now time.Time) []TaskInfo {
	tasks := []TaskInfo{
		{ID: "checkin", Enabled: !s.cfg.CheckinDisabled, Hours: s.cfg.CheckinHours},
		{ID: "travel", Enabled: !s.cfg.TravelDisabled, Hours: s.cfg.TravelHours},
		{ID: "activity", Enabled: !s.cfg.ActivityDisabled, Hours: s.cfg.ActivityHours},
		{ID: "keepalive", Enabled: !s.cfg.KeepaliveDisabled, Hours: s.cfg.KeepaliveHours},
		{ID: "school", Enabled: !s.cfg.SchoolDisabled, Hours: s.cfg.SchoolHours},
		{ID: "cat", Enabled: !s.cfg.CatDisabled, Hours: s.cfg.CatHours},
	}
	for i := range tasks {
		v := &tasks[i]
		v.Timezone = "Asia/Shanghai"
		v.Hours = append([]int{}, v.Hours...)
		if v.Enabled {
			at := nextFire(now.In(cstZone), v.Hours)
			if !at.IsZero() {
				v.NextAt = &at
			}
		}
	}
	return tasks
}

func (s *Scheduler) ExecuteTask(ctx context.Context, taskID string) (TaskResult, error) {
	found := false
	for _, v := range s.TaskCatalog(time.Now()) {
		if v.ID == taskID {
			found = true
			if !v.Enabled {
				return TaskResult{}, errors.New("task disabled")
			}
		}
	}
	if !found {
		return TaskResult{}, errors.New("unknown task")
	}
	if err := ctx.Err(); err != nil {
		return TaskResult{}, err
	}
	s.executeMu.Lock()
	defer s.executeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return TaskResult{}, err
	}
	result := TaskResult{Accounts: []AccountResult{}}
	if s.cfg.Pool == nil {
		return result, nil
	}
	s.observationMu.Lock()
	s.observation = &taskObservation{ctx: ctx, result: &result}
	s.observationMu.Unlock()
	defer func() { s.observationMu.Lock(); s.observation = nil; s.observationMu.Unlock() }()
	switch taskID {
	case "checkin":
		outcomes, err := s.CheckinAll()
		for _, v := range outcomes {
			r := AccountResult{UID: v.UID, Status: "unknown", Detail: "result_unconfirmed"}
			switch v.Status {
			case CheckinOK:
				r.Status, r.Detail = "success", "checkin_complete"
			case CheckinAlready:
				r.Status, r.Detail = "skipped", "already_claimed"
			case CheckinFail:
				r.Status, r.Detail = "failed", "checkin_failed"
			case CheckinSkipped:
				r.Status, r.Detail = "skipped", "ineligible"
				switch v.Detail {
				case "disabled", "global":
					r.Detail = v.Detail
				case "no credentials":
					r.Detail = "no_credentials"
				}
			}
			if v.Credits != nil {
				r.After = &Balance{Value: *v.Credits, ObservedAt: time.Now()}
			}
			result.Accounts = append(result.Accounts, r)
		}
		return result, err
	case "travel":
		s.RunTravelNow()
	case "activity":
		s.RunActivityNow()
	case "keepalive":
		s.RunKeepaliveNow()
	case "school":
		s.RunSchoolNow()
	case "cat":
		s.RunCatNow()
	}
	s.observationMu.Lock()
	err := s.observation.err
	s.observationMu.Unlock()
	return result, err
}

type taskObservation struct {
	ctx           context.Context
	result        *TaskResult
	err           error
	rewardInvalid map[string]bool
}

// Only fixed adapter classifications reach records; original upstream logs are not captured.
func (s *Scheduler) observe(r AccountResult) {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if s.observation == nil {
		return
	}
	out := s.observation.result
	// ponytail: linear account merge; add an index only if large pools make this measurable.
	for i := range out.Accounts {
		old := &out.Accounts[i]
		if old.UID != r.UID {
			continue
		}
		rank := map[string]int{"skipped": 1, "success": 2, "unknown": 3, "failed": 4}
		if rank[r.Status] > rank[old.Status] {
			old.Status, old.Detail = r.Status, r.Detail
		}
		if r.Before != nil && old.Before == nil {
			old.Before = r.Before
		}
		if r.After != nil {
			old.After = r.After
		}
		if r.Reward != nil && !s.observation.rewardInvalid[r.UID] {
			if old.Reward == nil {
				n := *r.Reward
				old.Reward = &n
			} else if *r.Reward > math.MaxInt64-*old.Reward {
				old.Reward = nil
				if old.Status != "failed" {
					old.Status, old.Detail = "unknown", "reward_unconfirmed"
				}
				if s.observation.rewardInvalid == nil {
					s.observation.rewardInvalid = map[string]bool{}
				}
				s.observation.rewardInvalid[r.UID] = true
			} else {
				*old.Reward += *r.Reward
			}
		}
		return
	}
	out.Accounts = append(out.Accounts, r)
}

func (s *Scheduler) scriptContext() context.Context {
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if s.coreContext != nil {
		return s.coreContext
	}
	if s.observation != nil {
		return s.observation.ctx
	}
	return context.Background() // Legacy one-shot CLI has no enclosing lifecycle.
}

func (s *Scheduler) scriptSnapshot() (dir string, uids []string, err error) {
	if s.cfg.Pool == nil {
		return "", nil, nil
	}
	snapshots := []*auth.Auth{}
	for _, st := range s.cfg.Pool.List() {
		a := s.cfg.Pool.AuthByUID(st.UID)
		var snapshot *auth.Auth
		if a != nil {
			snapshot = a.Snapshot()
		}
		detail := ""
		switch {
		case st.Disabled:
			detail = "disabled"
		case snapshot == nil:
			detail = "no_credentials"
		case snapshot.ActivationPending:
			detail = "pending_activation"
		case auth.ResolveRealm(snapshot.RealmStored(), snapshot.Domain) == "global":
			detail = "global"
		case strings.TrimSpace(snapshot.AccessToken) == "":
			detail = "no_credentials"
		}
		if detail != "" {
			s.observe(AccountResult{UID: st.UID, Status: "skipped", Detail: detail})
			continue
		}
		snapshots = append(snapshots, snapshot)
		uids = append(uids, st.UID)
	}
	if len(snapshots) == 0 {
		return "", nil, nil
	}
	dir, err = os.MkdirTemp("", "wb2a-task-auths-")
	if err != nil {
		return "", uids, err
	}
	for i, a := range snapshots {
		// ALL uses filename[10:18], not the JSON UID. Unique local slots avoid UID-prefix collisions.
		data, e := json.Marshal(map[string]any{"auth": map[string]any{"accessToken": a.AccessToken, "domain": a.Domain, "realm": a.RealmStored()}, "account": map[string]any{"uid": a.UID}})
		if e == nil {
			e = os.WriteFile(filepath.Join(dir, fmt.Sprintf("workbuddy-%08x.json", i)), data, 0600)
		}
		if e != nil {
			os.RemoveAll(dir)
			return "", uids, e
		}
	}
	return dir, uids, nil
}

func (s *Scheduler) scriptFailure(uids []string, detail string) {
	for _, uid := range uids {
		s.observe(AccountResult{UID: uid, Status: "failed", Detail: detail})
	}
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if s.observation != nil {
		s.observation.err = errors.New(detail)
	}
}

const taskLogLimit = 64 * 1024
const eventLineLimit = 4096
const omittedOutput = "上游输出已省略\n"

type taskOutput struct {
	mu       sync.Mutex
	s        *Scheduler
	allowed  map[string]bool
	seen     map[string]bool
	line     []byte
	overflow bool
}

func newTaskOutput(s *Scheduler, uids []string) *taskOutput {
	w := &taskOutput{s: s, allowed: map[string]bool{}, seen: map[string]bool{}}
	for _, uid := range uids {
		w.allowed[uid] = true
	}
	return w
}
func (w *taskOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			w.flush()
			continue
		}
		if len(w.line) < eventLineLimit && !w.overflow {
			w.line = append(w.line, b)
		} else {
			w.overflow = true
		}
	}
	return len(p), nil
}
func (w *taskOutput) flush() {
	defer func() { w.line = w.line[:0]; w.overflow = false }()
	if !w.overflow && bytes.HasPrefix(w.line, []byte("WB2A_TASK_EVENT ")) {
		var event struct {
			UID    string `json:"uid"`
			Status string `json:"status"`
			Detail string `json:"detail"`
			Reward *int64 `json:"reward"`
		}
		dec := json.NewDecoder(bytes.NewReader(w.line[len("WB2A_TASK_EVENT "):]))
		dec.DisallowUnknownFields()
		if dec.Decode(&event) == nil && dec.Decode(new(any)) == io.EOF && w.allowed[event.UID] && validTaskStatus(event.Status) && validTaskDetail(event.Detail) && (event.Reward == nil || *event.Reward >= 0) {
			w.s.observe(AccountResult{UID: event.UID, Status: event.Status, Detail: event.Detail, Reward: event.Reward})
			w.seen[event.UID] = true
			return
		}
	}
	w.s.observationMu.Lock()
	defer w.s.observationMu.Unlock()
	if w.s.observation == nil {
		return
	}
	r := w.s.observation.result
	if w.overflow {
		r.LogTruncated = true
	}
	if len(r.Log)+len(omittedOutput) <= taskLogLimit {
		r.Log += omittedOutput
	} else {
		r.LogTruncated = true
	}
}
func (w *taskOutput) finish(failed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.line) > 0 || w.overflow {
		w.flush()
	}
	for uid := range w.allowed {
		if !w.seen[uid] {
			r := AccountResult{UID: uid, Status: "unknown", Detail: "result_unconfirmed"}
			if failed {
				r.Status, r.Detail = "failed", "script_failed"
			}
			w.s.observe(r)
		}
	}
	if failed {
		w.s.scriptFailure(nil, "script_failed")
	}
}
func validTaskStatus(s string) bool {
	return s == "success" || s == "failed" || s == "skipped" || s == "unknown"
}
func validTaskDetail(s string) bool {
	switch s {
	case "reward_claimed", "already_claimed", "claim_failed", "outside_window", "activity_closed", "task_unavailable", "manual_required", "task_incomplete", "report_failed", "query_failed", "action_failed", "task_progress", "no_chances", "lottery_failed", "lottery_stalled", "global", "result_unconfirmed":
		return true
	}
	return false
}

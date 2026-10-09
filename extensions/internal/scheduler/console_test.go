package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/pool"
)

// Catches omitted catalog IDs, timezone drift, and disabled/unknown conflation.
func TestTaskCatalogUsesSchedulerConfig(t *testing.T) {
	s := New(Config{CheckinHours: []int{11}, TravelDisabled: true})
	now := time.Date(2026, 9, 15, 2, 0, 0, 0, time.UTC)
	list := s.TaskCatalog(now)
	ids := []string{}
	for _, v := range list {
		ids = append(ids, v.ID)
		if v.Timezone != "Asia/Shanghai" {
			t.Fatal(v)
		}
	}
	if !reflect.DeepEqual(ids, []string{"checkin", "travel", "activity", "keepalive", "school", "cat"}) {
		t.Fatal(ids)
	}
	if list[0].NextAt == nil || !list[0].NextAt.Equal(time.Date(2026, 9, 15, 3, 0, 0, 0, time.UTC)) {
		t.Fatal(list[0])
	}
	if list[1].Enabled || list[1].NextAt != nil {
		t.Fatal(list[1])
	}
	s.cfg.CheckinHours = nil
	v := s.TaskCatalog(now)[0]
	if !v.Enabled || v.NextAt != nil {
		t.Fatal(v)
	}
	list[2].Hours[0] = 5
	if s.cfg.ActivityHours[0] != 10 {
		t.Fatal("catalog leaked config slice")
	}
}

func TestRunScheduledUsesBeijingAndPreservesTaskOrder(t *testing.T) {
	old := schedulerNow
	calls := 0
	schedulerNow = func() time.Time {
		calls++
		if calls == 1 {
			return time.Date(2000, 1, 1, 2, 0, 0, 0, time.UTC)
		}
		return time.Now().Add(48 * time.Hour)
	}
	defer func() { schedulerNow = old }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ids := []string{}
	cfg := Config{Pool: pool.New(""), CheckinHours: []int{11}, TravelHours: []int{11}, ActivityHours: []int{11}, KeepaliveHours: []int{11}, SchoolHours: []int{11}, CatHours: []int{11}}
	cfg.Scheduled = func(got context.Context, id string, at time.Time) {
		if got != ctx || !at.Equal(time.Date(2000, 1, 1, 3, 0, 0, 0, time.UTC)) {
			t.Errorf("context/time %v %v", got, at)
		}
		ids = append(ids, id)
		if len(ids) == 6 {
			cancel()
		}
	}
	s := New(cfg)
	s.Run(ctx)
	if !reflect.DeepEqual(ids, []string{"checkin", "travel", "activity", "keepalive", "school", "cat"}) {
		t.Fatal(ids)
	}
	if s.taskID(taskKind(100)) != "" {
		t.Fatal("unknown kind accepted")
	}
}

func TestExecuteTravelObservesRealBranches(t *testing.T) {
	fastTravel(t)
	for _, tc := range []struct {
		state          string
		status, detail string
		reward         *int64
	}{
		{`{"state":"traveling"}`, "skipped", "traveling", nil},
		{`{"state":"idle","daily_limit_reached":true}`, "skipped", "daily_limit", nil},
		{`{"state":"idle"}`, "success", "departed", nil},
		{`{"state":"arrived","record_id":42}`, "success", "reward_claimed", ptrReward(9)},
		{`{"state":"new_state"}`, "unknown", "travel_state_unknown", nil},
	} {
		t.Run(tc.detail, func(t *testing.T) {
			stub := &travelStub{buddy: `{"id":1}`, state: tc.state}
			srv := stub.server()
			defer srv.Close()
			s, _ := newTravelScheduler(t, srv, "uid-one")
			result, err := s.ExecuteTask(context.Background(), "travel")
			if err != nil || len(result.Accounts) != 1 {
				t.Fatalf("%+v %v", result, err)
			}
			r := result.Accounts[0]
			if r.Status != tc.status || r.Detail != tc.detail || !reflect.DeepEqual(r.Reward, tc.reward) || r.After != nil {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func ptrReward(n int64) *int64 { return &n }

func TestTravelMissingOrZeroRewardRemainsUnknown(t *testing.T) {
	for _, data := range []string{`{}`, `{"reward_credit":0}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/claim") {
				fmt.Fprintf(w, `{"code":0,"data":%s}`, data)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/info") {
				fmt.Fprint(w, `{"code":0,"data":{"buddy":{"id":1}}}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"state":"arrived","record_id":1}}`)
		}))
		s, _ := newTravelScheduler(t, srv, "u1")
		r, err := s.ExecuteTask(context.Background(), "travel")
		srv.Close()
		if err != nil || len(r.Accounts) != 1 || r.Accounts[0].Status != "success" || r.Accounts[0].Reward != nil {
			t.Fatal(r, err)
		}
	}
}

func TestKeepaliveFailureAndSuccessArePerAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Refresh-Token") == "fail" {
			http.Error(w, "private-response", 403)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":{"accessToken":"new-access","expiresIn":3600}}`)
	}))
	defer srv.Close()
	s, p := newTravelScheduler(t, srv, "first", "second")
	for _, uid := range []string{"first", "second"} {
		a := p.AuthByUID(uid)
		a.FilePath = filepath.Join(t.TempDir(), "live.json")
		if uid == "first" {
			a.RefreshToken = "fail"
		}
	}
	r, err := s.ExecuteTask(context.Background(), "keepalive")
	if err != nil || len(r.Accounts) != 2 {
		t.Fatal(r, err)
	}
	got := map[string]string{}
	for _, a := range r.Accounts {
		got[a.UID] = a.Status
		if a.Reward != nil {
			t.Fatal(a)
		}
	}
	if got["first"] != "failed" || got["second"] != "success" {
		t.Fatal(got)
	}
}

func TestExecuteTaskObserverDoesNotLeakAcrossRuns(t *testing.T) {
	stub := &travelStub{buddy: `{"id":1}`, state: `{"state":"traveling"}`}
	srv := stub.server()
	defer srv.Close()
	s, _ := newTravelScheduler(t, srv, "u1")
	r, _ := s.ExecuteTask(context.Background(), "travel")
	s.observe(AccountResult{UID: "unrelated", Status: "failed", Detail: "report_failed"})
	r2, _ := s.ExecuteTask(context.Background(), "travel")
	if len(r.Accounts) != 1 || len(r2.Accounts) != 1 || r2.Accounts[0].UID != "u1" {
		t.Fatal(r, r2)
	}
}

func TestScriptLifecycleChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--lifecycle-ready" {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestScriptCommandCanceledWithCoreLifecycle(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	old := newScriptCmd
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	newScriptCmd = func(got context.Context, _ string, _ ...string) scriptRunner {
		if got != ctx {
			t.Error("lost lifecycle context")
		}
		return old(got, executable, "-test.run=^TestScriptLifecycleChild$", "--", "--lifecycle-ready", ready)
	}
	t.Cleanup(func() { newScriptCmd = old })
	s := New(Config{Pool: scriptPool(t)})
	var result TaskResult
	done := make(chan error, 1)
	go func() {
		var err error
		result, err = s.ExecuteTask(ctx, "cat")
		done <- err
	}()
	// Measure cancellation after a real child is ready. A cold system Python
	// launch on CI must not consume the shutdown deadline or skip Windows.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	startup := time.NewTimer(10 * time.Second)
	defer startup.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("child exited before readiness: %v", err)
		case <-startup.C:
			cancel()
			t.Fatal("child did not become ready")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled child succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child did not stop after cancellation")
	}
	for _, a := range result.Accounts {
		if a.Status != "failed" {
			t.Fatal(a)
		}
	}
}

func TestExecuteActivityFailureSurvivesLaterAdoption(t *testing.T) {
	fastTravel(t)
	old := activityAccountDelay
	activityAccountDelay = 0
	t.Cleanup(func() { activityAccountDelay = old })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/report"):
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		case strings.HasSuffix(r.URL.Path, "/streak"):
			http.Error(w, "private-body", 500)
		case strings.HasSuffix(r.URL.Path, "/buddy/info"):
			fmt.Fprint(w, `{"code":0,"data":{"buddy":null}}`)
		default:
			fmt.Fprint(w, `{"code":0,"data":{}}`)
		}
	}))
	defer srv.Close()
	s, _ := newTravelScheduler(t, srv, "uid-one", "uid-two")
	result, err := s.ExecuteTask(context.Background(), "activity")
	if err != nil || len(result.Accounts) != 2 {
		t.Fatalf("%+v %v", result, err)
	}
	for _, r := range result.Accounts {
		if r.Status != "failed" || r.Detail != "streak_failed" || r.Reward != nil {
			t.Fatalf("%+v", r)
		}
	}
}

func TestExecuteCheckinBalanceIsNotReward(t *testing.T) {
	srv := billingAndGrowthServer(&travelStub{})
	defer srv.Close()
	s, _ := newTravelScheduler(t, srv, "u1")
	result, err := s.ExecuteTask(context.Background(), "checkin")
	if err != nil || len(result.Accounts) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	r := result.Accounts[0]
	if r.Status != "success" || r.After == nil || r.After.Value != 500 || r.After.ObservedAt.IsZero() || r.Reward != nil {
		t.Fatalf("%+v", r)
	}
}

type observedScript struct {
	env    []string
	output io.Writer
	run    func(*observedScript) error
}

func (f *observedScript) SetDir(string)         {}
func (f *observedScript) SetEnv(e []string)     { f.env = e }
func (f *observedScript) SetOutput(w io.Writer) { f.output = w }
func (f *observedScript) Run() error            { return f.run(f) }
func scriptPool(t *testing.T) *pool.Pool {
	t.Helper()
	p := pool.New("")
	for _, uid := range []string{"same1234-one", "same1234-two"} {
		p.Add(&auth.Auth{UID: uid, AccessToken: "secret-access", RefreshToken: "secret-refresh", ExpiresAt: 9999999999, FilePath: filepath.Join(t.TempDir(), "live.json")})
	}
	return p
}
func installObservedScript(t *testing.T, run func(*observedScript) error) {
	t.Helper()
	old := newScriptCmd
	newScriptCmd = func(ctx context.Context, name string, args ...string) scriptRunner {
		if name != pythonCmd() || !reflect.DeepEqual(args, []string{"scripts/task_runner.py", "ALL", "--yes", "--only", "black_cat"}) {
			t.Fatalf("command %s %v", name, args)
		}
		if ctx.Value("core") != "lifecycle" {
			t.Fatal("lost core context")
		}
		return &observedScript{run: run}
	}
	t.Cleanup(func() { newScriptCmd = old })
}
func TestScriptUsesPrivateCNReadonlySnapshots(t *testing.T) {
	p := scriptPool(t)
	p.Add(&auth.Auth{UID: "global", Domain: "workbuddy.ai", AccessToken: "global-secret"})
	p.Add(&auth.Auth{UID: "pending", AccessToken: "pending-secret", ActivationPending: true})
	p.Add(&auth.Auth{UID: "disabled", AccessToken: "disabled-secret"})
	p.Disable("disabled", "test")
	auth.SetGlobalEnabled(false)
	t.Cleanup(func() { auth.SetGlobalEnabled(true) })
	var snapshotDir string
	installObservedScript(t, func(f *observedScript) error {
		for _, e := range f.env {
			if strings.HasPrefix(e, "WB2A_AUTHS=") {
				snapshotDir = strings.TrimPrefix(e, "WB2A_AUTHS=")
			}
		}
		info, err := os.Stat(snapshotDir)
		// Windows inherits the user's temporary-directory ACL; POSIX mode bits
		// are only meaningful on Unix. Keep the snapshot content/cleanup checks.
		if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0700) {
			t.Fatalf("snapshot mode %v %v", info, err)
		}
		files, _ := filepath.Glob(filepath.Join(snapshotDir, "workbuddy-*.json"))
		if len(files) != 2 {
			t.Fatal(files)
		}
		seen := map[string]bool{}
		prefix := map[string]bool{}
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			a, err := auth.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if a.AccessToken != "secret-access" || a.UID == "global" || a.UID == "pending" || a.UID == "disabled" {
				t.Fatal("unsafe snapshot")
			}
			seen[a.UID] = true
			prefix[filepath.Base(path)[10:18]] = true
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
				t.Fatalf("snapshot file %v %v", info, err)
			}
			fmt.Fprintf(f.output, "WB2A_TASK_EVENT {\"uid\":%q,\"status\":\"success\",\"detail\":\"reward_claimed\",\"reward\":7}\n", a.UID)
		}
		if !seen["same1234-one"] || !seen["same1234-two"] || len(prefix) != 2 {
			t.Fatal("ALL prefix collision")
		}
		return nil
	})
	s := New(Config{Pool: p})
	result, err := s.ExecuteTask(context.WithValue(context.Background(), "core", "lifecycle"), "cat")
	if err != nil || len(result.Accounts) != 5 {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(snapshotDir); !os.IsNotExist(err) {
		t.Fatal("snapshot not removed")
	}
	for _, uid := range []string{"same1234-one", "same1234-two"} {
		a := p.AuthByUID(uid).Snapshot()
		if a.AccessToken != "secret-access" {
			t.Fatal("live mutated")
		}
		if _, err := os.Stat(a.FilePath); !os.IsNotExist(err) {
			t.Fatal("live file written")
		}
	}
}

func TestScriptOutputNeverInfersSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		err          error
		want         string
		truncated    bool
	}{
		{"exit zero", "ok token=secret\n", nil, "unknown", false},
		{"invalid event", `WB2A_TASK_EVENT {"uid":"same1234-one","status":"success","detail":"reward_claimed","reward":1,"cookie":"secret"}` + "\n", nil, "unknown", false},
		{"failure sticks", "WB2A_TASK_EVENT {\"uid\":\"same1234-one\",\"status\":\"failed\",\"detail\":\"claim_failed\",\"reward\":null}\nWB2A_TASK_EVENT {\"uid\":\"same1234-one\",\"status\":\"success\",\"detail\":\"reward_claimed\",\"reward\":9}\n", nil, "failed", false},
		{"nonzero", "", errors.New("secret command error"), "failed", false},
		{"oversize", strings.Repeat("token-cookie-body", 10000), nil, "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installObservedScript(t, func(f *observedScript) error { io.WriteString(f.output, tc.output); return tc.err })
			s := New(Config{Pool: scriptPool(t)})
			r, err := s.ExecuteTask(context.WithValue(context.Background(), "core", "lifecycle"), "cat")
			if (err != nil) != (tc.err != nil) {
				t.Fatal(err)
			}
			for _, v := range r.Accounts {
				if v.UID == "same1234-one" && v.Status != tc.want {
					t.Fatalf("%+v", v)
				}
			}
			if strings.Contains(r.Log, "secret") || strings.Contains(r.Log, "token") || len(r.Log) > 65536 || r.LogTruncated != tc.truncated {
				t.Fatalf("unsafe/bound log %q", r.Log)
			}
		})
	}
}

func TestScriptNoEligibleAccountsNeverStarts(t *testing.T) {
	old := newScriptCmd
	newScriptCmd = func(context.Context, string, ...string) scriptRunner { t.Fatal("child started"); return nil }
	t.Cleanup(func() { newScriptCmd = old })
	for _, p := range []*pool.Pool{nil, pool.New("")} {
		s := New(Config{Pool: p})
		for _, id := range []string{"school", "cat"} {
			r, err := s.ExecuteTask(context.Background(), id)
			if err != nil || len(r.Accounts) != 0 {
				t.Fatal(r, err)
			}
		}
	}
}

func TestNonzeroExitPreservesExplicitAccountResults(t *testing.T) {
	for _, second := range []string{"success", "failed"} {
		t.Run(second, func(t *testing.T) {
			installObservedScript(t, func(f *observedScript) error {
				io.WriteString(f.output, "WB2A_TASK_EVENT {\"uid\":\"same1234-one\",\"status\":\"success\",\"detail\":\"reward_claimed\",\"reward\":7}\n")
				fmt.Fprintf(f.output, "WB2A_TASK_EVENT {\"uid\":\"same1234-two\",\"status\":%q,\"detail\":\"claim_failed\",\"reward\":null}\n", second)
				return errors.New("exit2 private-body")
			})
			s := New(Config{Pool: scriptPool(t)})
			r, err := s.ExecuteTask(context.WithValue(context.Background(), "core", "lifecycle"), "cat")
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("missing fixed process error", err)
			}
			got := map[string]string{}
			for _, a := range r.Accounts {
				got[a.UID] = a.Status
			}
			if got["same1234-one"] != "success" || got["same1234-two"] != second {
				t.Fatal(got)
			}
		})
	}
}

func TestScriptRewardOverflowIsUnknown(t *testing.T) {
	installObservedScript(t, func(f *observedScript) error {
		for _, reward := range []string{"9223372036854775807", "1", "3"} {
			fmt.Fprintf(f.output, "WB2A_TASK_EVENT {\"uid\":\"same1234-one\",\"status\":\"success\",\"detail\":\"reward_claimed\",\"reward\":%s}\n", reward)
		}
		return nil
	})
	s := New(Config{Pool: scriptPool(t)})
	r, err := s.ExecuteTask(context.WithValue(context.Background(), "core", "lifecycle"), "cat")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range r.Accounts {
		if a.UID == "same1234-one" && (a.Reward != nil || a.Status != "unknown") {
			t.Fatal(a)
		}
	}
}

func TestExecuteTaskRejectsUnknownAndDisabled(t *testing.T) {
	s := New(Config{TravelDisabled: true})
	for _, id := range []string{"../school", "travel"} {
		if _, err := s.ExecuteTask(context.Background(), id); err == nil {
			t.Fatal(id)
		}
	}
	raw, err := json.Marshal(AccountResult{UID: "u", Status: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	json.Unmarshal(raw, &v)
	for _, key := range []string{"before", "after", "reward"} {
		value, ok := v[key]
		if !ok || value != nil {
			t.Fatal(string(raw))
		}
	}
}

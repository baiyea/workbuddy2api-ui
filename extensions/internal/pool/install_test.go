package pool

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"workbuddy2api/internal/auth"
)

func TestInstallPersistsBeforePublishingAndKeepsExistingIdentity(t *testing.T) {
	p := New("")
	defer p.Close()
	dir := t.TempDir()
	a := &auth.Auth{UID: "one", AccessToken: "old", RefreshToken: "old-r", FilePath: filepath.Join(dir, "workbuddy-one.json")}
	if err := p.Install(a); err != nil {
		t.Fatal(err)
	}
	p.Disable("one", sessionDeadReason)
	fresh := &auth.Auth{UID: "one", AccessToken: "new", RefreshToken: "new-r", FilePath: a.FilePath}
	if err := p.Install(fresh); err != nil {
		t.Fatal(err)
	}
	if p.AuthByUID("one") != a {
		t.Fatal("replaced live pointer; stale refresh can overwrite login")
	}
	if err := a.SaveAtomic(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(a.FilePath)
	disk, err := auth.Parse(raw)
	if err != nil || disk.AccessToken != "new" {
		t.Fatal("old token restored")
	}
	st, _ := p.Status("one")
	if st.Disabled {
		t.Fatal("session-dead account not revived")
	}
	bad := &auth.Auth{UID: "two", AccessToken: "token", FilePath: filepath.Join(dir, "missing", "two.json")}
	if p.Install(bad) == nil || p.AuthByUID("two") != nil {
		t.Fatal("published account before persistence")
	}
}

func TestCreditsDistinguishUnobservedFromZero(t *testing.T) {
	p := New("")
	defer p.Close()
	p.Add(&auth.Auth{UID: "one", AccessToken: "token"})
	st, _ := p.Status("one")
	if st.CreditsKnown {
		t.Fatal("new account balance falsely known")
	}
	p.ReenableIfCredits("one", 0)
	st, _ = p.Status("one")
	if !st.CreditsKnown || st.Credits != 0 {
		t.Fatal("observed zero not distinguished")
	}
}

func TestInstallRevivesOnlyReloginRequiredDisables(t *testing.T) {
	for _, reason := range []string{sessionDeadReason, "account banned by upstream (11140 request illegal), re-login required", "operator disabled"} {
		t.Run(reason, func(t *testing.T) {
			p := New("")
			defer p.Close()
			a := &auth.Auth{UID: "one", AccessToken: "old", FilePath: filepath.Join(t.TempDir(), "workbuddy-one.json")}
			if err := p.Install(a); err != nil {
				t.Fatal(err)
			}
			p.Disable(a.UID, reason)
			if err := p.Install(&auth.Auth{UID: a.UID, AccessToken: "new"}); err != nil {
				t.Fatal(err)
			}
			st, _ := p.Status(a.UID)
			if st.Disabled != (reason == "operator disabled") {
				t.Fatalf("unexpected disabled=%v", st.Disabled)
			}
		})
	}
}

func TestPendingActivationIsUnavailableAcrossRestart(t *testing.T) {
	a, err := auth.Parse([]byte(`{"uid":"global-one","accessToken":"token","realm":"global","activation_pending":true}`))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	a.FilePath = filepath.Join(dir, "workbuddy-global-one.json")
	p := New("")
	defer p.Close()
	if err := p.Install(a); err != nil {
		t.Fatal(err)
	}
	if p.ServableNow() {
		t.Fatal("pending account is routable")
	}
	p.Cooldown(a.UID, CoolSoft, time.Minute, "old limit")
	if p.Pick("") != nil {
		t.Fatal("pending account entered cooldown fallback")
	}
	loaded, err := auth.LoadDir(dir)
	if err != nil || len(loaded) != 1 {
		t.Fatal("lost saved pending credentials")
	}
	restarted := New("")
	defer restarted.Close()
	restarted.SyncToDir(loaded)
	st, _ := restarted.Status(a.UID)
	if restarted.ServableNow() || !st.Disabled {
		t.Fatal("restart enabled unfinished account")
	}
	if err := loaded[0].CompleteActivation("stale-token"); err == nil {
		t.Fatal("stale login activated account")
	}
	if err := loaded[0].CompleteActivation("token"); err != nil {
		t.Fatal(err)
	}
	if !restarted.ServableNow() {
		t.Fatal("completed activation remains unavailable")
	}
	loaded, err = auth.LoadDir(dir)
	if err != nil || loaded[0].PendingActivation() {
		t.Fatal("activation completion not persisted")
	}
}

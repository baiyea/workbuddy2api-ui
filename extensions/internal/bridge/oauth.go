package bridge

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/oauth"
	"workbuddy2api/internal/upstream"
)

type loginFlow struct {
	mu                                      sync.Mutex
	id, owner, state, link, status, message string
	expires, lastPoll                       time.Time
	client                                  *oauth.Client
	account                                 *auth.Auth
	countries                               []oauth.Country
	cancel                                  context.CancelFunc
	ctx                                     context.Context
}

func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("system random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (h *handler) cleanupLocked() {
	for id, f := range h.flows {
		if time.Now().After(f.expires) || h.ctx.Err() != nil {
			f.cancel()
			delete(h.flows, id)
		}
	}
}

func (h *handler) cancelOwner(w http.ResponseWriter, r *http.Request) {
	owner := r.Header.Get("X-Console-Owner")
	if r.PathValue("owner") != owner {
		bridgeError(w, 404, "授权流程不存在")
		return
	}
	h.mu.Lock()
	for id, f := range h.flows {
		if f.owner == owner {
			f.cancel()
			delete(h.flows, id)
		}
	}
	h.mu.Unlock()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *handler) startLogin(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Realm string `json:"realm"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	if b.Realm != "cn" && b.Realm != "global" {
		bridgeError(w, 400, "请选择国内版或国际版")
		return
	}
	if b.Realm == "global" && !h.cfg.GlobalEnabled {
		bridgeError(w, 400, "此部署未开启国际版")
		return
	}
	if h.cfg.Pool == nil || h.cfg.Upstream == nil {
		bridgeError(w, 503, "授权服务尚未就绪")
		return
	}
	c, err := h.newOAuth(b.Realm)
	if err != nil {
		bridgeError(w, 400, err.Error())
		return
	}
	a := h
	a.mu.Lock()
	a.cleanupLocked()
	if len(a.flows) >= 8 {
		a.mu.Unlock()
		bridgeError(w, 429, "授权流程较多，请等待旧流程结束或过期")
		return
	}
	id := randomSecret()
	expiry := time.Now().Add(10 * time.Minute)
	ctx, cancel := context.WithDeadline(h.ctx, expiry)
	f := &loginFlow{id: id, owner: r.Header.Get("X-Console-Owner"), expires: expiry, status: "waiting", client: c, ctx: ctx, cancel: cancel}
	f.mu.Lock()
	a.flows[id] = f
	a.mu.Unlock()
	defer f.mu.Unlock()
	f.state, f.link, err = c.Start(ctx)
	if err != nil || !oauth.ValidAuthorizationURL(f.link) {
		cancel()
		a.mu.Lock()
		delete(a.flows, id)
		a.mu.Unlock()
		if err == nil {
			err = errors.New("上游返回了不受信任的授权链接")
		}
		bridgeError(w, 502, err.Error())
		return
	}
	f.message = "请在打开的上游页面完成登录，此处会自动等待结果"
	f.respond(w)
}
func (h *handler) ownedFlow(w http.ResponseWriter, r *http.Request) *loginFlow {
	a := h
	a.mu.Lock()
	a.cleanupLocked()
	f := a.flows[r.PathValue("id")]
	a.mu.Unlock()
	if f == nil || f.owner != r.Header.Get("X-Console-Owner") {
		bridgeError(w, 404, "授权流程已过期，请重新添加账号")
		return nil
	}
	return f
}
func (f *loginFlow) respond(w http.ResponseWriter) {
	out := map[string]any{"id": f.id, "status": f.status, "message": f.message, "expires_at": f.expires, "auth_url": f.link}
	if len(f.countries) > 0 {
		out["countries"] = f.countries
	}
	if f.account != nil {
		out["uid"] = f.account.UID
		out["nickname"] = f.account.Nickname
	}
	writeJSON(w, 200, out)
}
func (h *handler) pollLogin(w http.ResponseWriter, r *http.Request) {
	f := h.ownedFlow(w, r)
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status == "complete" || f.status == "needs_region" || f.status == "failed" {
		f.respond(w)
		return
	}
	if time.Since(f.lastPoll) < 2*time.Second {
		f.respond(w)
		return
	}
	f.lastPoll = time.Now()
	if f.account == nil {
		acct, err := f.client.Poll(f.ctx, f.state)
		if err != nil {
			f.message = err.Error()
			if !errors.Is(err, oauth.ErrPending) && !errors.Is(err, oauth.ErrTemporary) {
				f.status = "failed"
			}
			f.respond(w)
			return
		}
		acct.FilePath = filepath.Join(h.cfg.AuthDir, "workbuddy-"+acct.UID+".json")
		acct.ActivationPending = f.client.Realm == "global"
		// Capture this flow's credentials before Install can publish acct as live.
		private := acct.Snapshot()
		h.mu.Lock()
		if h.flows[f.id] != f || f.ctx.Err() != nil || time.Now().After(f.expires) {
			h.mu.Unlock()
			bridgeError(w, 404, "授权流程已过期，请重新添加账号")
			return
		}
		err = h.cfg.Pool.Install(acct)
		h.mu.Unlock()
		if err != nil {
			f.status = "failed"
			f.message = "账号保存失败，请检查数据卷权限或账号版本是否冲突"
			f.respond(w)
			return
		}
		f.account = private
	}
	h.finishLogin(f)
	f.respond(w)
}
func (h *handler) finishLogin(f *loginFlow) {
	if f.ctx.Err() != nil {
		return
	}
	a := f.account
	if f.client.Realm == "global" {
		needs, err := f.client.Registration(f.ctx, a)
		if err != nil {
			f.status = "retry"
			f.message = "凭据已保存，注册激活失败：" + err.Error()
			return
		}
		if needs {
			f.countries, err = f.client.Countries(f.ctx)
			if err != nil {
				f.status = "retry"
				f.message = "凭据已保存，地区列表获取失败：" + err.Error()
				return
			}
			f.status = "needs_region"
			f.message = "账号需要完善注册地区，请选择你的地区"
			return
		}
		if _, err := h.cfg.Upstream.ClaimTrialContext(f.ctx, a); err != nil {
			f.status = "retry"
			f.message = "凭据已保存，试用额度激活未成功；稍后自动重试"
			return
		}
		if f.ctx.Err() != nil {
			return
		}
		live := h.cfg.Pool.AuthByUID(a.UID)
		if live == nil {
			f.status, f.message = "failed", "账号已被移除，请重新添加"
			return
		}
		if err := live.CompleteActivation(a.AccessToken); err != nil {
			f.status, f.message = "retry", "激活状态保存失败，请重新添加账号"
			return
		}
	} else {
		// Sign-in is ancillary: a failure does not discard a valid authorized account.
		if err := h.cfg.Upstream.DailyCheckinContext(f.ctx, a); err != nil && !upstream.IsAlreadyCheckin(err) {
			f.message = "账号已保存，签到暂未成功"
		}
	}
	if f.ctx.Err() != nil {
		return
	}
	if remain, err := h.cfg.Upstream.UserResourceContext(f.ctx, a); err == nil && f.ctx.Err() == nil {
		h.cfg.Pool.SetCredits(a.UID, remain)
	}
	if f.ctx.Err() != nil {
		return
	}
	f.status = "complete"
	if f.message == "" || !strings.HasPrefix(f.message, "账号已保存，签到") {
		f.message = "账号已添加，可前往对话测试；实际可用性以模型返回为准"
	}
	f.cancel()
}
func (h *handler) completeRegion(w http.ResponseWriter, r *http.Request) {
	f := h.ownedFlow(w, r)
	if f == nil {
		return
	}
	var b struct {
		Region string `json:"region"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status != "needs_region" {
		bridgeError(w, 409, "当前流程无需选择地区")
		return
	}
	for _, country := range f.countries {
		if country.IOS2 == b.Region {
			if err := f.client.CompleteRegion(f.ctx, f.account, country); err != nil {
				f.message = err.Error()
				f.respond(w)
				return
			}
			f.countries = nil
			f.status = "retry"
			h.finishLogin(f)
			f.respond(w)
			return
		}
	}
	bridgeError(w, 400, "请选择列表中的地区")
}

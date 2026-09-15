package pool

import (
	"errors"
	"workbuddy2api/internal/auth"
)

// Install persists first and preserves the live Auth pointer on reauthorization.
// Existing refresh/save callers therefore see the newly installed credentials.
func (p *Pool) Install(a *auth.Auth) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.byUID[a.UID]; ok {
		if e.a.Realm() != a.Realm() {
			return errors.New("同一账号 ID 已属于另一个版本")
		}
		if err := e.a.ReplaceAndSave(a); err != nil {
			return err
		}
		if e.disabled && (e.reason == sessionDeadReason || e.reason == "account banned by upstream (11140 request illegal), re-login required") {
			e.disabled = false
			e.reason = ""
			e.sessionDeadFails = 0
		}
	} else {
		if err := a.SaveAtomic(); err != nil {
			return err
		}
		p.upsertLocked(a)
	}
	p.dirty.Store(true)
	return nil
}

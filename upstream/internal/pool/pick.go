// 选号：Pick 簇（healthy 三因子加权 Top5 短名单 + 加权随机 + 全冷却兜底 + 在途占满过滤）。
package pool

import (
	"log"
	"math/rand/v2"
	"sort"
	"time"

	"workbuddy2api/internal/auth"
	"workbuddy2api/internal/logfmt"
)

// Pick 单一选号入口（无请求级轮换、无 realm 过滤，模型感知）。
// 挑选策略：healthy 账号中按三因子权重取前 5 名，再在 Top5 内按同一权重加权随机抽签，
// 意图是打散热点，避免永远打同一个账号。
// model 非空时启用 6004 模型级冷却豁免（healthyForModel）；空则等价账号级 healthy。
// 需要请求级轮换（tried）或分池（realm）时用 PickExcludingForRealm。
func (p *Pool) Pick(model string) *auth.Auth {
	return p.pick(nil, model, "")
}

// pick 在 healthy 候选集中按三因子权重加权随机选出账号，并记录 lastUsed（防并发撞号）。
// 候选集是 top5 近似：先按三因子权重（weightOf）降序取前 5（credits 只是权重的一个因子，
// 闲置补偿与成功率同样决定谁进短名单），再在 top5 内做防撞号过滤。
// 并发防雪崩：跳过 lastUsed 距今 < minPickGap 的账号（除非 top5 全部刚被用过，
// 此时退回最近最少使用 LRU 账号），迫使高并发请求发散，而不是全部撞同一高分账号。
// minPickGap=0（测试用）时过滤恒通过，退化为纯加权随机。
// reqModel 非空时把健康口径换成 healthyForModel（6004 模型豁免生效；PickExcluding 传 ""）。
// realm 非空时候选过滤叠加 Realm()==realm 谓词（分池选号域；PickExcluding 传 ""）。
// 注意：模型豁免只进 normal 选号（候选 healthy 判定）；全冷却兜底不参与模型豁免——
// 兜底本来就是在"无任何 direct 可用"时的降级，切模型可用性已在 normal 阶段体现。
func (p *Pool) pick(tried map[string]bool, reqModel, realm string) *auth.Auth {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	// 惰性清理过期的 6004 模型级冷却（map 不无限膨胀；status 只读遍历跳过过期项）。
	for _, e := range p.byUID {
		e.pruneExpiredModelCooldowns(now)
	}
	realmOK := func(e *entry) bool { return realm == "" || e.a.Realm() == realm }
	healthyOf := func(e *entry) bool { return realmOK(e) && e.healthy(now) }
	if reqModel != "" {
		healthyOf = func(e *entry) bool { return realmOK(e) && e.healthyForModel(now, reqModel) }
	}
	var cands []*entry
	for uid, e := range p.byUID {
		if tried != nil && tried[uid] {
			continue
		}
		if !healthyOf(e) {
			continue
		}
		if p.inFlightFull(e) {
			continue // 在途占满：跳过（max=0 不限时不触发）
		}
		cands = append(cands, e)
	}
	if len(cands) == 0 {
		// 全冷却兜底：无 healthy 候选时，从冷却账号里选 until 最早到期的一个
		// （熔断/冷却共用 expiry 口径，取较早截止者）。禁用的账号永不参与兜底。
		return p.pickEarliestExpiryLocked(tried, now, realm)
	}
	// top5 短名单按三因子权重降序截断（而非 credits 单纯降序）：否则闲置补偿 + 成功率
	// 根本进不了短名单决策，低 credits 但高成功率/久置的账号会永远排不进 top5。
	var maxCredits int64
	for _, e := range cands {
		if e.credits > maxCredits {
			maxCredits = e.credits
		}
	}
	// 权重只算一次：顶 5 截断要排序，若在 sort 比较器里现算 weightOf 会翻成 O(n log n) 次
	// 冗余浮点计算（46 账号约 500 次）。先做 O(n) 预计算，再按 (权重, uid) 排序。
	type weighted struct {
		e *entry
		w float64
	}
	// 成本分层：reqModel 非空时，按该模型的实测扣费把候选分层，只保留最优层。
	//   0 = 已实测免费（限免期/夜间免费的号，最强偏好）
	//   1 = 无观测（含观测过期）
	//   2 = 已实测收费
	// 为什么"无观测"排在"已实测收费"之前：新号的限免状态只能靠实测发现，
	// 若已知收费的号恒压过未知号，那台免费的号永远轮不到，也就永远学不到。
	// 为什么用硬过滤而非仅排序：pickWeighted 会在候选内加权随机，只排序的话
	// 收费号仍有机会抽中，达不到"优先免费"的语义。
	costTier := func(e *entry) (int, float64) {
		mc, ok := e.modelCostOf(reqModel, now)
		if !ok {
			return 1, 0
		}
		if mc.CostPer1k <= 0 {
			return 0, 0
		}
		return 2, mc.CostPer1k
	}
	bestTier := 2
	for _, e := range cands {
		if ti, _ := costTier(e); ti < bestTier {
			bestTier = ti
		}
	}
	ws := make([]weighted, 0, len(cands))
	for _, e := range cands {
		if ti, _ := costTier(e); ti == bestTier {
			ws = append(ws, weighted{e: e, w: p.weightOf(e, maxCredits, now)})
		}
	}
	// 等权重洗牌：仅当存在权重相等且候选数超过 top5 时，才对 ws 做 Fisher-Yates
	// 洗牌（且**不消耗 p.randInt64N 注入源**，避免改变 pickWeighted 的确定性语义，
	// 见 TestPickDeterministicViaSetRandomSource）。权重全等或存在并列时，按字典序
	// 截断会让 uid 靠后的账号永远进不了 top5（惊群测试 c00 集中 79/100 的根因：
	// c05..c09 被字典序截断、LRU 兜底又只在 top5 内转）。洗牌用独立的 time-seeded
	// 源，只在截断边界制造等权重随机次序，不影响加权抽签本身的确定性。
	if len(ws) > 5 {
		eq := false
		for i := 1; i < len(ws); i++ {
			if ws[i].w == ws[0].w {
				eq = true
				break
			}
		}
		if eq {
			shuf := rand.New(rand.NewPCG(uint64(now.UnixNano()), uint64(len(ws))))
			shuf.Shuffle(len(ws), func(i, j int) { ws[i], ws[j] = ws[j], ws[i] })
		}
	}
	sort.SliceStable(ws, func(i, j int) bool {
		_, ci := costTier(ws[i].e)
		_, cj := costTier(ws[j].e)
		if ci != cj {
			return ci < cj // 收费层：单价低的在前
		}
		if ws[i].w != ws[j].w {
			return ws[i].w > ws[j].w
		}
		return ws[i].e.a.UID < ws[j].e.a.UID // 稳定兜底（洗牌后此项几乎不触发）
	})
	cands = cands[:0]
	for _, c := range ws {
		cands = append(cands, c.e)
	}
	// candsAll 保留截断前的全候选（权重降序），供 LRU 兜底在全量范围选最旧者，
	// 避免 top5 字典序截断把等权重靠后账号饿死（惊群根因之一）。
	candsAll := cands
	if len(cands) > 5 {
		cands = cands[:5]
	}
	// 防并发撞号：在持锁内基于「上次选中时刻」过滤，但同一批并发 goroutine 会串行进入
	// 本函数（写锁），每个进入者都把 lastUsed 置为 now —— 于是同一瞬间的第 2..N 个
	// 进入者看到前一个账号 lastUsed==now（距今 0 < minPickGap），被自然挤向其他账号。
	// 关键：lastUsed 在锁内赋值，使时间窗口判定在并发下可重入（此前 Acquire 在锁外，
	// 多个 goroutine 在窗口内同时通过校验造成惊群，TestPickAntiThunderingHerd 实证）。
	eligible := make([]*entry, 0, len(cands))
	for _, e := range cands {
		if now.Sub(e.lastUsed) >= minPickGap {
			eligible = append(eligible, e)
		}
	}
	var e *entry
	if len(eligible) == 0 {
		// top5 全部刚被用过：LRU 兜底，在**全候选 candsAll**（非仅 top5）里选最旧者。
		// 用 usedSeq 单调序号而非 lastUsed 墙钟比较：Windows 等平台 time.Now() 精度
		// ~0.5ms，快速连续选号时所有 lastUsed 完全相等，Before 全 false 会恒选
		// candsAll[0] 导致集中。usedSeq 严格全序，与时间精度无关。
		e = candsAll[0]
		for _, c := range candsAll[1:] {
			if c.usedSeq < e.usedSeq {
				e = c
			}
		}
	} else {
		e = p.pickWeighted(eligible) // eligible 保序 = top5 降序子集
	}
	e.lastUsed = now // 锁内即时标记：下一个进入 pick 的 goroutine 立即看到本号已用
	p.pickSeq++
	e.usedSeq = p.pickSeq // 单调序号：保证 usedSeq 严格全序（防惊群/LRU 的权威依据）
	return e.a
}

// pickEarliestExpiryLocked 全冷却兜底：在非禁用的软冷却/熔断账号中选截止最早的一个。
// 分级：disabled 永不参与；CoolHard（余额耗尽，等签到的号）同样排除——调了必 402，浪费轮换并产生噪音日志；
// CoolSoft 与熔断号允许参与（可能已恢复，失败成本仅一轮换）。
// 被 tried 排除、在途占满的账号同样跳过（维持请求级轮换 + 租约语义）。无任何可用返回 nil。
func (p *Pool) pickEarliestExpiryLocked(tried map[string]bool, now time.Time, realm string) *auth.Auth {
	var best *entry
	for uid, e := range p.byUID {
		if tried != nil && tried[uid] {
			continue
		}
		if realm != "" && e.a.Realm() != realm {
			continue // 域过滤：池内跨 realm 的冷却账号不参与本 realm 兜底
		}
		if e.disabled {
			continue // 禁用的账号永不参与兜底
		}
		if e.coolKind == CoolHard && !e.until.IsZero() && now.Before(e.until) {
			continue // 余额耗尽号（处于有效 hard 冷却期）不参与兜底：等签到恢复，调了必 402
		}
		if p.inFlightFull(e) {
			continue
		}
		exp := e.expiry(now)
		if exp.IsZero() {
			continue
		}
		if best == nil || exp.Before(best.expiry(now)) {
			best = e
		}
	}
	if best == nil {
		return nil
	}
	log.Printf("WARN: [pool] fallback_earliest_expiry uid=%s until=%s kind=%s", logfmt.UID8(best.a.UID), best.expiry(now).Format(time.RFC3339), best.fallbackKind(now))
	best.lastUsed = time.Now()
	return best.a
}

// inFlightFull 报告账号是否已占满在途名额（max=0 不限 → 恒 false）。
// 调用方需已持 p.mu（读锁或写锁均可，本方法只读 p.maxInFlight）。
func (p *Pool) inFlightFull(e *entry) bool {
	if p.maxInFlight <= 0 {
		return false
	}
	return e.inFlight.Load() >= int64(p.maxInFlight)
}

// minPickGap 防并发撞号窗口：同一账号在该窗口内不重复被选中（除非 top5 全部刚被用过）。
// 生产默认 100ms；纯加权分布测试可临时置 0 关闭防撞号。
var minPickGap = 100 * time.Millisecond

// expiringWeight 快过期积分占比的权重系数（三因子之外的第四因子）。
// 取 8：略低于 credits 总量项（×10），足以在"快过期多"与"总量相近"的号之间拉开差距，
// 又不至于压过总量项让"总量大但快过期少"的号被完全饿死。
const expiringWeight = 8.0

// pickWeighted 三因子加权随机（claude-api selectWeightedRandom 参考口径）：
//
//		weight = credits 比例 × 10 + idleWeight + successRate × 3
//
//	  - credits 比例 = 该号 credits / 候选集内最大 credits（避免量纲爆炸）
//	  - idleWeight = min(距 lastUsed 小时数 × idleWeightPerHour, idleWeightMax)；从未使用给满分
//	  - successRate = successCount/(successCount+errTotal)；无请求记录给 1.5（中性偏信任）
//
// credits 全 0 时仍按 idle+successRate 加权（不退化均匀随机）。
// 权重为浮点，用 int64 定点（×1e6）抽签可保持确定性随机源注入（randInt64N 语义不变）。
// 随机源优先用 p.randInt64N（仅供测试注入确定性），nil 时回退 math/rand/v2 全局源。
func (p *Pool) pickWeighted(cands []*entry) *entry {
	now := time.Now()
	var maxCredits int64
	for _, e := range cands {
		if e.credits > maxCredits {
			maxCredits = e.credits
		}
	}
	const scale = 1_000_000 // 定点放大：int64 累加权重大整数抽签
	weights := make([]int64, len(cands))
	var total int64
	for i, e := range cands {
		w := p.weightOf(e, maxCredits, now)
		// 四舍五入并保底权重 ≥1：向零截断会让 w<1/scale 的低权重号权重归零，
		// 彻底失去被抽中机会（候选少时加剧选号集中，惊群测试的放大因子之一）。
		wi := int64(w*scale + 0.5)
		if wi < 1 {
			wi = 1
		}
		weights[i] = wi
		total += wi
	}
	rnd := rand.Int64N
	if p.randInt64N != nil {
		rnd = p.randInt64N
	}
	if total <= 0 {
		return cands[int(rnd(int64(len(cands))))]
	}
	r := rnd(total)
	var acc int64
	for i, e := range cands {
		acc += weights[i]
		if r < acc {
			return e
		}
	}
	return cands[len(cands)-1]
}

// weightOf 计算单个账号的三因子权重。
func (p *Pool) weightOf(e *entry, maxCredits int64, now time.Time) float64 {
	w := 1.0
	// 1. credits 比例 ×10（会计入 mid-credit 锚点，避免全员 0 时 credits 项为 0）。
	if maxCredits > 0 {
		w += float64(e.credits) / float64(maxCredits) * 10
	}
	// 1b. 快过期积分加成（issue:积分过期）：官方活动赠送的奖励积分按批过期，
	// 不用就作废。creditsExpiring 占总量比例越高，越应优先被消耗——把"快过期
	// 占比"作为一个独立的强权重项（×expiringWeight），让快过期积分多的号优先选。
	// 与成本分层（costTier 优先免费）正交：那是按"实测扣费"分层，这是按"过期紧迫度"。
	if e.credits > 0 && e.creditsExpiring > 0 {
		w += float64(e.creditsExpiring) / float64(e.credits) * expiringWeight
	}
	// 2. 闲置补偿。
	if e.lastUsed.IsZero() {
		w += p.idleWeightMax // 从未使用 → 满分
	} else {
		hours := now.Sub(e.lastUsed).Hours()
		idleW := hours * p.idleWeightPerHour
		if idleW > p.idleWeightMax {
			idleW = p.idleWeightMax
		}
		if idleW < 0 {
			idleW = 0 // lastUsed 在未来（时钟回拨）时钳 0
		}
		w += idleW
	}
	// 3. 成功率 ×3。
	totalReq := e.successCount + e.errTotal
	if totalReq > 0 {
		w += float64(e.successCount) / float64(totalReq) * 3
	} else {
		w += 1.5 // 无请求记录 → 中性偏信任
	}
	return w
}

// SetCredits 更新账号余额。

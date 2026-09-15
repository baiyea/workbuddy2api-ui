// global 模型名目录探测：只产模型名，不产倍率（PLAN §3.D2「模型名目录 ≠ 倍率表」）。
//
// credits 数值一律不进入本包实现——探测端点即便返回倍率字段也忽略，名单只喂
// /v1/models 的 global: 前缀输出，不注入 costTier、不参与选号。
package upstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"workbuddy2api/internal/auth"
)

// GlobalModelNames 国际版（global realm）模型名静态名单兜底（PLAN §7.2 附录 21 名）。
// 只含模型名、不含倍率。探测失败 / 无 global 账号时直接输出此名单；
// 探测成功时以其 "权威 21 名" 为基底，追加探测独有的模型名（去重）。
var GlobalModelNames = []string{
	"default-model",
	"fast-model",
	"balanced-model",
	"primary-model",
	"hy4-preview",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"deep-model",
	"deepseek-v4.1-flash",
	"gpt-6-astra",
	"hy4-preview-f",
	"hy3",
	"glm-5.2",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.3-codex",
	"gemini-3.5-flash",
	"glm-5.3",
	"kimi-k3",
	"kimi-k2.6",
}

// fetchGlobalModelsCache 探测结果缓存（语义参照 CN 侧 handler.dynamicModelsCache：1h TTL +
// 5min 失败负缓存）。按 Client 实例持有（effortsMu 同模式），测试新建 Client 即隔离。
// Mutex 内嵌，与 modelList 无并发读路径竞争（唯一读写点本文件内）。
type fetchGlobalModelsCache struct {
	sync.Mutex
	names    []string // 成功缓存：探测 ∪ 静态名单（已去重）；nil = 未探测
	fetched  time.Time
	lastFail time.Time
}

// globalModelsTTL / globalModelsFailCooldown 探测缓存时长：成功 1h，失败 5min 负缓存。
const (
	globalModelsTTL          = time.Hour
	globalModelsFailCooldown = 5 * time.Minute
)

// globalModelsProbePaths global 模型目录端点候选序列（按 realm 切 base，路径"家族"）：
// /v2 家族优先（PR #20 实测 /v2/enterprises/personal/models 200 含完整模型表），
// /console 作 fallback（同域旧路径，或 500）。参考 PLAN v1 §2.2 分歧③ 与
// rockswang/wild-work PR #20 实测结论：console 路径在 global 上非 200 → 先 /v2。
var globalModelsProbePaths = []string{
	"/v2/enterprises/personal/models",
	"/console/enterprises/personal/models",
}

// FetchGlobalModels 探测 global 账号的模型名目录并返回模型名列表（含 context 无关、无倍率）。
//
// 成功：探测结果 ∪ GlobalModelNames（去重，静态 21 为基底，探测独有追加），缓存 1h。
// 失败（家族端点全非 2xx / 解析失败 / 空列表）：记 5min 负缓存，回落 GlobalModelNames。
// 缓存/负缓存命中：直接返回，零上游调用。
//
// 调用方负责：① 仅在有 global 账号时调用（无则不探测）；
// ② GlobalEnabled 关闭时（逃生门）不得调用——本方法由 globalOn(a) 内部兜底，若账号
// 因开关回落 cn 则返回 nil（handler 侧回落静态名单，仍零探测）。
func (c *Client) FetchGlobalModels(a *auth.Auth) []string {
	if !c.globalOn(a) {
		// 逃生门兜底：账号不路由 global 上游 → 不探测，回落静态名单（零上游调用）。
		return GlobalModelNames
	}

	c.globalModels.Lock()
	if len(c.globalModels.names) > 0 && time.Since(c.globalModels.fetched) < globalModelsTTL {
		out := c.globalModels.names
		c.globalModels.Unlock()
		return out
	}
	if !c.globalModels.lastFail.IsZero() && time.Since(c.globalModels.lastFail) < globalModelsFailCooldown {
		// 负缓存冷却期内：避免反复打上游，直接按失败处理（回落静态）。
		c.globalModels.Unlock()
		return GlobalModelNames
	}
	c.globalModels.Unlock()

	names, efforts, defaults, err := c.probeGlobalModels(a)
	if err != nil || len(names) == 0 {
		// 探测失败：负缓存 + 回落静态名单（effort 桶不写，prepareBody 走 globalEffortMap 静态兜底）。
		c.globalModels.Lock()
		c.globalModels.lastFail = time.Now()
		c.globalModels.names = nil
		c.globalModels.Unlock()
		return GlobalModelNames
	}
	// global 域 effort 能力：探测下发的 supportedEfforts/defaultEffort 权威写入 global 桶
	// （raw remote，不并入静态表——静态兜底在 prepareBody 的 globalEffortMap 与
	// /v1/models 的 EffortListing 里按需 fallback）。空探测不写（防清既有桶）。
	if len(efforts) > 0 || len(defaults) > 0 {
		c.storeEfforts("global", efforts, defaults)
	}

	// 成功：静态名单为基底，追加探测独有（去重）。只取名字，倍率字段忽略。
	seen := make(map[string]bool, len(GlobalModelNames)+len(names))
	merged := make([]string, 0, len(GlobalModelNames)+len(names))
	for _, id := range GlobalModelNames {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		merged = append(merged, id)
	}
	for _, id := range names {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		merged = append(merged, id)
	}

	c.globalModels.Lock()
	c.globalModels.names = merged
	c.globalModels.fetched = time.Now()
	c.globalModels.lastFail = time.Time{}
	c.globalModels.Unlock()
	return merged
}

// probeGlobalModels 按候选路径序列发起一次探测，返回模型名列表（未去重、已滤 disabled）
// 及解析出的 effort 能力桶（supportedEfforts/defaultEffort，可为空）。家族端点全部非 2xx
// （等幂探活）才返回错误。
func (c *Client) probeGlobalModels(a *auth.Auth) (names []string, efforts map[string][]string, defaults map[string]string, err error) {
	var lastErr error
	for _, path := range globalModelsProbePaths {
		names, efforts, defaults, err = c.globalModelsOnce(a, path)
		if err != nil {
			lastErr = err
			continue
		}
		return names, efforts, defaults, nil
	}
	return nil, nil, nil, lastErr
}

// globalModelsOnce 单端点探测。2xx + 解析出非空名单 → (names, efforts, defaults, nil)；否则 (nil,...,err)。
func (c *Client) globalModelsOnce(a *auth.Auth, path string) ([]string, map[string][]string, map[string]string, error) {
	url := c.chatBase(a) + path // 按 realm 切 base：global 账号 → global base
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	c.CommonHeaders(req, a) // 共享请求头（Origin/Referer/UA），与 FetchModels 同款
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("global models status %d: %s", resp.StatusCode, truncate(string(raw), 120))
	}
	return parseGlobalModelNames(raw)
}

// globalModelEntry 对象形态单条模型字段（含 reasoning 档位桶，对齐 CN FetchModels 解析口径）。
type globalModelEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	// 上游或仅下发 reasoning.effort（单档字符串）而非 supportedEfforts 数组——两形态都读，
	// 数组优先。缺 reasoning / 缺档位 → 桶为空（调用方不写、回落静态兜底）。
	Reasoning struct {
		Effort           string   `json:"effort"`
		DefaultEffort    string   `json:"defaultEffort"`
		SupportedEfforts []string `json:"supportedEfforts"`
	} `json:"reasoning"`
}

// parseGlobalModelNames 容忍两种形态解析模型名：
//   - 对象数组：data.models[].id/.name（id 优先），disabled 剔除；
//   - 窄表：data 为字符串数组。
//
// 对象形态额外解析 reasoning.supportedEfforts / defaultEffort（P0：global 域 effort 探测，
// 解析不到时调用方回落 staticEffortCap 兜底表——prepareBody 的 globalEffortMap 与
// /v1/models 的 EffortListing）。窄表形态无元数据 → 桶为空。
//
// 解析成功但名单为空 → 返回错误（调用方回落静态，等价"该端点没给全"）。
func parseGlobalModelNames(raw []byte) (names []string, efforts map[string][]string, defaults map[string]string, err error) {
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, nil, nil, fmt.Errorf("global models parse: %w", err)
	}
	if env.Code != 0 {
		return nil, nil, nil, fmt.Errorf("global models code=%d", env.Code)
	}
	trimmed := strings.TrimSpace(string(env.Data))
	if strings.HasPrefix(trimmed, "[") {
		// 窄表形态：data 为字符串数组（无 effort 元数据）。
		var arr []string
		if err := json.Unmarshal(env.Data, &arr); err != nil {
			return nil, nil, nil, fmt.Errorf("global models parse (narrow): %w", err)
		}
		out := make([]string, 0, len(arr))
		for _, id := range arr {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
		if len(out) == 0 {
			return nil, nil, nil, fmt.Errorf("global models empty list")
		}
		return out, nil, nil, nil
	}
	// 对象形态：data.models[].id/.name（id 优先），disabled 剔除，附带 parsing reasoning。
	var obj struct {
		Models []globalModelEntry `json:"models"`
	}
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		return nil, nil, nil, fmt.Errorf("global models parse: %w", err)
	}
	out := make([]string, 0, len(obj.Models))
	for _, m := range obj.Models {
		id := m.ID
		if id == "" {
			id = m.Name
		}
		if id == "" || m.Disabled {
			continue
		}
		out = append(out, id)
		// effort 桶：supportedEfforts 数组优先；缺数组但 reasoning.effort 单档非空 → 视作单档表。
		if len(m.Reasoning.SupportedEfforts) > 0 {
			if efforts == nil {
				efforts = make(map[string][]string)
			}
			efforts[id] = m.Reasoning.SupportedEfforts
		} else if e := strings.TrimSpace(m.Reasoning.Effort); e != "" {
			if efforts == nil {
				efforts = make(map[string][]string)
			}
			efforts[id] = []string{e}
		}
		if d := strings.TrimSpace(m.Reasoning.DefaultEffort); d != "" {
			if defaults == nil {
				defaults = make(map[string]string)
			}
			defaults[id] = d
		}
	}
	if len(out) == 0 {
		return nil, nil, nil, fmt.Errorf("global models empty list")
	}
	return out, efforts, defaults, nil
}

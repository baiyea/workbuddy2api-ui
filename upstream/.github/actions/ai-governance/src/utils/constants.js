/**
 * 应用常量定义
 */

// 检测决策类型
const DECISIONS = {
  SPAM: 'SPAM',
  README_COVERED: 'README_COVERED',
  BASIC: 'BASIC',
  UNCLEAR: 'UNCLEAR',
  KEEP: 'KEEP',
  INVALID_COMMIT: 'INVALID_COMMIT',
  MALICIOUS: 'MALICIOUS',
  TRIVIAL: 'TRIVIAL'
};

// AI响应类型
const AI_RESPONSES = {
  SPAM: 'SPAM',
  NOT_SPAM: 'NOT_SPAM',
  COVERED: 'COVERED',
  NOT_COVERED: 'NOT_COVERED',
  UNCLEAR: 'UNCLEAR',
  BASIC: 'BASIC',
  VALID: 'VALID',
  INVALID: 'INVALID',
  MALICIOUS: 'MALICIOUS',
  TRIVIAL: 'TRIVIAL',
  RELATED: 'RELATED',
  NOT_RELATED: 'NOT_RELATED'
};

// 检测步骤
const DETECTION_STEPS = {
  SPAM_CHECK: 1,
  COVERAGE_CHECK: 2,
  QUALITY_CHECK: 3
};

// GitHub事件类型
const GITHUB_EVENTS = {
  ISSUES: 'issues',
  PULL_REQUEST_TARGET: 'pull_request_target',
  OPENED: 'opened'
};

// 分析深度级别
const ANALYSIS_DEPTHS = {
  LIGHT: 'light',
  NORMAL: 'normal',
  DEEP: 'deep'
};

// 治理判定值（AI 归一化返回）
const GOVERNANCE_DECISIONS = {
  DUPLICATE: 'DUPLICATE',
  NEW_TOPIC: 'NEW_TOPIC',
  UNCERTAIN: 'UNCERTAIN',
  WELL_FORMED: 'WELL_FORMED'
};

// 默认配置值
const DEFAULTS = {
  MAX_TOKENS: 100,
  TEMPERATURE: 0.1,
  MAX_FILES: 5,
  MAX_PATCH_LINES: 5,
  AI_MODEL: 'openai/gpt-4o',
  ANALYSIS_DEPTH: 'normal',
  LOCK_REASON: 'spam'
};

// 治理默认值（camelCase 与 issueGovernanceService / issueHandler 内部使用一致）
const GOVERNANCE_DEFAULTS = {
  canonicalLabel: 'canonical',
  duplicateLabel: 'duplicate',
  maxCanonicalIndex: 50,
  dryRun: true,
  maintainerExempt: true,
  enablePrGovernance: false,
  canonicalBodyTruncate: 1500,
  // 判定为「规范」的确定性完整性门槛（模板命中 + 有效段落数 + 标题/正文长度）
  wellFormedMinSections: 3,
  wellFormedMinTitleLen: 8,
  wellFormedMinBodyLen: 80,
  // 永远豁免的账号（bot 自环防护）
  SKIP_USERS: ['github-actions[bot]', 'github-actions']
};

module.exports = {
  DECISIONS,
  AI_RESPONSES,
  DETECTION_STEPS,
  GITHUB_EVENTS,
  ANALYSIS_DEPTHS,
  DEFAULTS,
  GOVERNANCE_DECISIONS,
  GOVERNANCE_DEFAULTS
};

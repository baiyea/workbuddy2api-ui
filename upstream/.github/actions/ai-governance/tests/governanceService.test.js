const baseConfig = require('../config.json');
const { applyLocale } = require('../src/utils/config');
const IssueGovernanceService = require('../src/services/issueGovernanceService');
const { GOVERNANCE_DECISIONS } = require('../src/utils/constants');

function buildConfig() {
  const config = JSON.parse(JSON.stringify(baseConfig));
  applyLocale(config, 'zh-CN');
  return config;
}

// 构造一个 openai stub：chat.completions.create 按 purpose 序列返回预设结果
function makeOpenai(resultsByCall = []) {
  const create = jest.fn();
  resultsByCall.forEach(res => create.mockResolvedValueOnce({
    choices: [{ message: { content: res } }]
  }));
  return {
    chat: { completions: { create } },
    _create: create
  };
}

// 构造 github ops mock
function makeOps({ canonicalItems = [] } = {}) {
  return {
    listCanonicalIssues: jest.fn().mockResolvedValue(canonicalItems),
    createIssue: jest.fn().mockResolvedValue({ data: { number: 99, html_url: 'https://github.com/o/r/issues/99' } }),
    addComment: jest.fn().mockResolvedValue({}),
    addLabels: jest.fn().mockResolvedValue({}),
    updateIssueState: jest.fn().mockResolvedValue({})
  };
}

const issue = {
  number: 22,
  title: '请求支持某种能力',
  body: '希望网关支持 xxx，目前做不到。',
  user: { login: 'someone' }
};

describe('IssueGovernanceService', () => {
  test('DUPLICATE 归并：打 duplicate 标签、评论、关闭原 issue，并在 canonical 追加记录', async () => {
    const config = buildConfig();
    // 依次: extract(structured) -> well_formed -> merge_match
    const openai = makeOpenai([
      '```json\n{"要点":"支持 xxx","要做的事":["实现 xxx"]}\n```',
      'WELL_FORMED',
      'DUPLICATE(#57)'
    ]);
    const ops = makeOps({ canonicalItems: [{ number: 57, title: '已有 xxx 能力', body: '...' }] });
    const gov = new IssueGovernanceService(openai, 'model', config, { dryRun: false }, ops);
    const octokit = {}; // ops 是 mock，octokit 不会被真正调用

    const result = await gov.govern(octokit, 'o', 'r', issue, 'enhancement');

    expect(result).toMatchObject({
      decision: GOVERNANCE_DECISIONS.DUPLICATE,
      canonicalNumber: 57,
      closed: true
    });
    expect(ops.addLabels).toHaveBeenCalledWith(octokit, 'o', 'r', 22, ['duplicate'], expect.any(String));
    expect(ops.updateIssueState).toHaveBeenCalledWith(octokit, 'o', 'r', 22, 'closed', 'not_planned');
    // canonical 上追加归并记录：来源 issue 编号、作者、要点
    const recordCall = ops.addComment.mock.calls.find(c => c[3] === 57);
    expect(recordCall).toBeTruthy();
    expect(recordCall[4]).toContain('#22');
    expect(recordCall[4]).toContain('@someone');
    // 原 issue 评论指向 canonical
    const mergeCall = ops.addComment.mock.calls.find(c => c[3] === 22);
    expect(mergeCall[4]).toContain('#57');
  });

  test('NEW_TOPIC 且非规范：创建 canonical，关闭原 issue', async () => {
    const config = buildConfig();
    const openai = makeOpenai([
      '```json\n{"要点":"新需求 yyy","要做的事":["做 yyy"]}\n```',
      'NEEDS_NORMALIZE',
      'NEW_TOPIC',
      '[Feature] 新需求 yyy\n\n## 概述\n支持 yyy。\n\n## 背景与要点\n- 做 yyy'
    ]);
    const ops = makeOps();
    const gov = new IssueGovernanceService(openai, 'model', config, { dryRun: false }, ops);
    const octokit = {};

    const result = await gov.govern(octokit, 'o', 'r', issue, 'enhancement');

    expect(result.decision).toBe(GOVERNANCE_DECISIONS.NEW_TOPIC);
    expect(ops.createIssue).toHaveBeenCalled();
    const createArgs = ops.createIssue.mock.calls[0];
    expect(createArgs[3]).toMatch(/^\[Feature\]/); // 标题带前缀
    // 创建 canonical 成功，才允许关闭原 issue
    expect(ops.updateIssueState).toHaveBeenCalledWith(octokit, 'o', 'r', 22, 'closed', 'not_planned');
  });

  test('创建 canonical 失败时绝不关闭原 issue（先建后关的顺序保证）', async () => {
    const config = buildConfig();
    const openai = makeOpenai([
      '```json\n{"要点":"新需求","要做的事":[]}\n```',
      'NEEDS_NORMALIZE',
      'NEW_TOPIC',
      '[Feature] x\n\n## 概述\nx'
    ]);
    const ops = makeOps();
    ops.createIssue.mockRejectedValueOnce(new Error('boom'));
    const gov = new IssueGovernanceService(openai, 'model', config, { dryRun: false }, ops);

    await expect(gov.govern({}, 'o', 'r', issue, null)).rejects.toThrow('boom');
    expect(ops.updateIssueState).not.toHaveBeenCalled();
  });

  test('UNCERTAIN 时谨慎放行：只评论，不关闭', async () => {
    const config = buildConfig();
    const openai = makeOpenai([
      '```json\n{"要点":"模糊","要做的事":[]}\n```',
      'NEEDS_NORMALIZE',
      'UNCERTAIN'
    ]);
    const ops = makeOps({ canonicalItems: [{ number: 1, title: 'x', body: 'x' }] });
    const gov = new IssueGovernanceService(openai, 'model', config, { dryRun: false }, ops);
    const octokit = {};

    const result = await gov.govern(octokit, 'o', 'r', issue, null);

    expect(result.decision).toBe(GOVERNANCE_DECISIONS.UNCERTAIN);
    expect(result.passed).toBe(true);
    expect(ops.updateIssueState).not.toHaveBeenCalled();
    expect(ops.addComment).toHaveBeenCalled();
  });

  test('dry-run 模式：不关闭、不创建、不打标签，仍执行分析与评论', async () => {
    const config = buildConfig();
    const openai = makeOpenai([
      '```json\n{"要点":"重复需求","要做的事":[]}\n```',
      'NEEDS_NORMALIZE',
      'DUPLICATE(#57)'
    ]);
    const ops = makeOps({ canonicalItems: [{ number: 57, title: 'x', body: 'x' }] });
    const gov = new IssueGovernanceService(openai, 'model', config, { dryRun: true }, ops);
    const octokit = {};

    const result = await gov.govern(octokit, 'o', 'r', issue, null);

    expect(result.dryRun).toBe(true);
    expect(ops.createIssue).not.toHaveBeenCalled();
    expect(ops.updateIssueState).not.toHaveBeenCalled();
    expect(ops.addLabels).not.toHaveBeenCalled();
    // dry-run 仍会发一条说明评论
    expect(ops.addComment).toHaveBeenCalled();
  });

  test('canonical 索引为空时直接视为新主题，不发起归并匹配调用', async () => {
    const config = buildConfig();
    // 若跳过匹配，第三段 NEW_TOPIC 不应被消费；这里只准备 extract + well_formed 两段
    const openai = makeOpenai([
      '```json\n{"要点":"a","要做的事":[]}\n```',
      'NEEDS_NORMALIZE'
    ]);
    const ops = makeOps({ canonicalItems: [] });
    const gov = new IssueGovernanceService(openai, 'model', config, { dryRun: true }, ops);
    const octokit = {};

    await gov.govern(octokit, 'o', 'r', issue, null);

    expect(openai._create).toHaveBeenCalledTimes(2); // extract + well_formed
  });
});
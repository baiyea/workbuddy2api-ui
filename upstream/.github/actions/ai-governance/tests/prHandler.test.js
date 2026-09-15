const { handleNewPR, isCollaborator } = require('../src/handlers/prHandler');
const { applyLocale } = require('../src/utils/config');
const baseConfig = require('../config.json');

jest.mock('../src/services/prWorkflowService');
jest.mock('../src/services/prGovernanceService');

const PrWorkflowService = require('../src/services/prWorkflowService');
const PrGovernanceService = require('../src/services/prGovernanceService');

function buildConfig() {
  const config = JSON.parse(JSON.stringify(baseConfig));
  applyLocale(config, 'zh-CN');
  return config;
}

const govDefaults = { dryRun: false, maintainerExempt: true };

function makePR(overrides = {}) {
  return {
    number: 42,
    title: 'feat: add caching',
    body: 'body',
    user: { login: 'someone' },
    ...overrides
  };
}

function makeContext(pr) {
  return {
    payload: { pull_request: pr, action: 'opened' },
    eventName: 'pull_request_target'
  };
}

function makeOctokit({ isCollaborator = false } = {}) {
  const checkCollaborator = jest.fn();
  if (isCollaborator) {
    checkCollaborator.mockResolvedValue({ status: 204 });
  } else {
    checkCollaborator.mockRejectedValue(new Error('not a collaborator'));
  }
  return {
    rest: {
      repos: { checkCollaborator },
      pulls: {
        listFiles: jest.fn().mockResolvedValue({ data: [] }),
        update: jest.fn().mockResolvedValue({})
      },
      issues: { createComment: jest.fn().mockResolvedValue({}) }
    }
  };
}

describe('prHandler', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    PrWorkflowService.mockImplementation(() => ({
      performLayeredDetection: jest.fn().mockResolvedValue({ decision: 'KEEP' }),
      classifyAndLabelPR: jest.fn().mockResolvedValue('enhancement'),
      config: buildConfig()
    }));
    PrGovernanceService.mockImplementation(() => ({
      govern: jest.fn().mockResolvedValue({})
    }));
  });

  test('维护者豁免：协作者 PR 做垃圾检测 + 分类，但不进入治理', async () => {
    const config = buildConfig();
    const pr = makePR();
    const octokit = makeOctokit({ isCollaborator: true });
    const gov = { ...govDefaults };

    await handleNewPR(octokit, {}, makeContext(pr), 'o', 'r', 'model', config, ['enhancement'], [], gov);

    // 垃圾检测仍执行
    expect(PrWorkflowService).toHaveBeenCalled();
    // 不调用治理
    expect(PrGovernanceService).not.toHaveBeenCalled();
  });

  test('跳过名单（bot 自环）：直接返回，不做任何检测与治理', async () => {
    const config = buildConfig();
    const pr = makePR({ user: { login: 'github-actions[bot]' } });
    const octokit = makeOctokit();
    const gov = { ...govDefaults };

    await handleNewPR(octokit, {}, makeContext(pr), 'o', 'r', 'model', config, ['enhancement'], [], gov);

    expect(PrWorkflowService).not.toHaveBeenCalled();
    expect(PrGovernanceService).not.toHaveBeenCalled();
  });

  test('AI 失败放行：治理抛错时只评论放行，不关闭 PR', async () => {
    const config = buildConfig();
    const pr = makePR();
    const octokit = makeOctokit();
    const gov = { ...govDefaults };

    // 治理服务 govern 抛错
    PrGovernanceService.mockImplementation(() => ({
      govern: jest.fn().mockRejectedValue(new Error('AI boom'))
    }));

    await handleNewPR(octokit, {}, makeContext(pr), 'o', 'r', 'model', config, ['enhancement'], [], gov);

    // 不调用关闭（closePR 会走 octokit.rest.pulls.update({state:'closed'})）
    expect(octokit.rest.pulls.update).not.toHaveBeenCalled();
  });

  test('isCollaborator：查询失败按非协作者处理（宁可多治理）', async () => {
    const octokit = makeOctokit({ isCollaborator: false });
    await expect(isCollaborator(octokit, 'o', 'r', 'someone')).resolves.toBe(false);
  });
});
const core = require('@actions/core');
const github = require('@actions/github');
const OpenAI = require('openai');

const { loadConfig, parseInputs } = require('./utils/config');
const { logMessage } = require('./utils/helpers');
const { handleNewIssue } = require('./handlers/issueHandler');
const { handleNewPR } = require('./handlers/prHandler');
const { GOVERNANCE_DEFAULTS } = require('./utils/constants');

/**
 * 主程序入口
 */
async function run() {
  try {
    // 加载配置文件
    const baseConfig = loadConfig();

    // 解析输入参数（含治理层新增参数）
    const {
      token,
      aiModel,
      labelsList,
      blacklistUsers,
      analyzeFileChanges,
      analysisDepth,
      maxFilesToAnalyze,
      maxPatchLinesPerFile,
      customBaseUrl,
      customApiKey,
      canonicalLabel,
      duplicateLabel,
      skipUsers,
      dryRun,
      maintainerExempt,
      enablePrGovernance,
      maxCanonicalIndex,
      canonicalBodyTruncate,
      config
    } = parseInputs(baseConfig);

    // 初始化GitHub客户端
    const octokit = github.getOctokit(token);
    const context = github.context;

    // 确定使用的API配置
    const apiBaseUrl = customBaseUrl || config.defaults.api_base_url;
    const apiKey = customApiKey || token;

    // 初始化OpenAI客户端
    const openai = new OpenAI({
      baseURL: apiBaseUrl,
      apiKey: apiKey
    });

    // 输出配置信息
    if (customBaseUrl) {
      core.info(config.logging.using_custom_api);
    } else {
      core.info(config.logging.using_github_models);
    }
    core.info(logMessage(config.logging.using_ai_model, { model: aiModel }));
    core.info(config.logging.config_info);
    core.info(logMessage(config.logging.analysis_depth_info, { analyze_changes: analyzeFileChanges }));
    core.info(logMessage(config.logging.analysis_depth_details, {
      depth: analysisDepth,
      files: maxFilesToAnalyze,
      lines: maxPatchLinesPerFile
    }));

    // 获取仓库信息
    const { owner, repo } = context.repo;

    // 治理参数打包
    const gov = {
      canonicalLabel,
      duplicateLabel,
      skipUsers,
      dryRun,
      maintainerExempt,
      enablePrGovernance,
      maxCanonicalIndex,
      canonicalBodyTruncate
    };

    // 根据事件类型处理
    if (context.eventName === 'issues' && context.payload.action === 'opened') {
      await handleNewIssue(octokit, openai, context, owner, repo, aiModel, config, labelsList, blacklistUsers, gov);
    } else if ((context.eventName === 'pull_request_target') && context.payload.action === 'opened') {
      if (enablePrGovernance) {
        // PR 治理：上游垃圾检测（唯一会关 PR 的路径）+ 要点提炼 + canonical 关联（永不关闭合法 PR）
        await handleNewPR(octokit, openai, context, owner, repo, aiModel, config, labelsList, blacklistUsers, gov);
      } else {
        core.info('PR 治理默认关闭（enable-pr-governance=false），跳过');
      }
    } else {
      core.info(config.logging.event_no_match);
    }

  } catch (error) {
    core.setFailed(error.message);
  }
}

if (require.main === module) {
  run();
}

module.exports = { run, GOVERNANCE_DEFAULTS };
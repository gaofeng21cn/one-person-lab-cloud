#!/usr/bin/env node

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildOplWhitepaper } from './opl-whitepaper-builder.ts';

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

buildOplWhitepaper({
  repoRoot,
  sourceMarkdown: 'docs/whitepapers/opl-cloud-whitepaper.md',
  outputName: 'opl-cloud-whitepaper',
  status: 'opl_cloud_whitepaper_ready',
  owner: 'One Person Lab',
  coverLine: 'OPL Gateway / OPL Workspace / OPL Console / OPL Fabric / OPL Ledger',
  headerTitle: 'OPL Cloud Whitepaper',
  minSections: 8,
  minPdfPages: 8,
  requiredSections: [
    '## 定位摘要',
    '## OPL Cloud 的五大设计原则',
    '## OPL Cloud 的能力版图',
    '## 标准任务生命周期',
    '## 三类用户路径',
    '## 系统模型',
    '## 五类信任机制',
    '## 本文边界',
    '## 结语',
  ],
  requiredTerms: [
    'OPL Cloud 白皮书',
    'OPL Gateway',
    'OPL App',
    'OPL Workspace',
    'OPL Console',
    'OPL Fabric',
    'OPL Ledger',
    'OPL Connect',
    'OPL Compute',
    'OPL Environments',
    'OPL Agent Registry',
    '计划',
    '批准',
    '执行',
    '监控',
    '回收',
    '回执',
    'OPL Cloud 的五大设计原则',
    'OPL Cloud 的能力版图',
    '标准任务生命周期',
    '三类用户路径',
    '系统模型',
    '专业工作流',
    '五类信任机制',
    '本文边界',
    '结语',
  ],
});

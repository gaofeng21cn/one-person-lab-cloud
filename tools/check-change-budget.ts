#!/usr/bin/env node
/**
 * Change budget checker.
 *
 * Measures what a change adds and deletes, and reports freeze-list violations
 * for the temporary change budget in docs/policies/change-budget.md.
 *
 * Usage:
 *   node tools/check-change-budget.ts [--base origin/main] [--enforce] [--exception] [--summary]
 *
 * Exit codes:
 *   0  ok or warn
 *   1  fail (only with --enforce)
 */

import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

export type FileClass = 'generated' | 'test' | 'code' | 'doc' | 'other';

export interface DiffEntry {
  path: string;
  status: 'A' | 'M' | 'D';
  added: number;
  deleted: number;
  fileClass: FileClass;
}

export interface Summary {
  entries: DiffEntry[];
  newFiles: number;
  deletedFiles: number;
  codeAdded: number;
  codeDeleted: number;
  generatedAdded: number;
  generatedDeleted: number;
  testAdded: number;
  testDeleted: number;
  docAdded: number;
  docDeleted: number;
  netCode: number;
}

export interface Violation {
  rule: string;
  detail: string;
}

const CODE_EXT = new Set(['go', 'ts', 'tsx', 'js', 'mjs', 'py', 'sh', 'sql', 'css', 'html', 'proto']);
const DOC_EXT = new Set(['md', 'json', 'yaml', 'yml', 'txt', 'toml', 'example']);
const GENERATED_HEADER = /(code generated|do not edit|auto-generated|@generated)/i;
const TEST_BASENAME = /(_test\.go|\.test\.(ts|tsx|js|mjs)|\.spec\.(ts|tsx|js)|_test\.py|^test_.*\.py|\.test-.*)$/;
const ABSTRACTION_NAME = /(reconcile|readback|receipt|fence|admission|settlement)/i;
const ABSTRACTION_DIR = /(coordination|reconcile|readback|receipt|fence|admission|settlement)/i;

function extensionOf(file: string): string {
  const base = file.split('/').pop() ?? file;
  const dot = base.lastIndexOf('.');
  return dot === -1 ? '' : base.slice(dot + 1).toLowerCase();
}

export function classifyFile(file: string, head: string): FileClass {
  const lower = file.toLowerCase();
  const parts = lower.split('/');
  const base = parts[parts.length - 1] ?? lower;

  if (head && GENERATED_HEADER.test(head)) return 'generated';
  if (parts[0] === 'tests' || parts.includes('testdata') || parts.includes('__tests__') || parts.includes('fixtures')) {
    return 'test';
  }
  if (TEST_BASENAME.test(base)) return 'test';

  const ext = extensionOf(file);
  if (CODE_EXT.has(ext)) return 'code';
  if (DOC_EXT.has(ext)) return 'doc';
  return 'other';
}

export function summarize(entries: DiffEntry[]): Summary {
  const summary: Summary = {
    entries,
    newFiles: 0,
    deletedFiles: 0,
    codeAdded: 0,
    codeDeleted: 0,
    generatedAdded: 0,
    generatedDeleted: 0,
    testAdded: 0,
    testDeleted: 0,
    docAdded: 0,
    docDeleted: 0,
    netCode: 0,
  };

  for (const entry of entries) {
    if (entry.status === 'A') summary.newFiles += 1;
    if (entry.status === 'D') summary.deletedFiles += 1;

    switch (entry.fileClass) {
      case 'code':
        summary.codeAdded += entry.added;
        summary.codeDeleted += entry.deleted;
        break;
      case 'generated':
        summary.generatedAdded += entry.added;
        summary.generatedDeleted += entry.deleted;
        break;
      case 'test':
        summary.testAdded += entry.added;
        summary.testDeleted += entry.deleted;
        break;
      case 'doc':
        summary.docAdded += entry.added;
        summary.docDeleted += entry.deleted;
        break;
      default:
        break;
    }
  }

  summary.netCode = summary.codeAdded - summary.codeDeleted;
  return summary;
}

export function detectFreezeViolations(entries: DiffEntry[], addedRpcLines: number, newDirs: Set<string> = new Set()): Violation[] {
  const violations: Violation[] = [];

  for (const entry of entries) {
    if (entry.status !== 'A') continue;

    const base = entry.path.split('/').pop() ?? entry.path;
    const dir = entry.path.includes('/') ? entry.path.slice(0, entry.path.lastIndexOf('/')) : '';
    if (entry.fileClass === 'code' && (ABSTRACTION_NAME.test(base) || (newDirs.has(dir) && ABSTRACTION_DIR.test(dir)))) {
      violations.push({ rule: 'new-abstraction-file', detail: entry.path });
    }
    if (/\/ent\/schema\/[^/]+\.go$/.test(entry.path)) {
      violations.push({ rule: 'new-ent-schema', detail: entry.path });
    }
    if (/^\.github\/workflows\//.test(entry.path)) {
      violations.push({ rule: 'new-workflow', detail: entry.path });
    }
    if (/^docs\/spec\/target\//.test(entry.path)) {
      violations.push({ rule: 'new-target-spec', detail: entry.path });
    }
    if (/\/cmd\/[^/]+\/main\.go$/.test(entry.path)) {
      violations.push({ rule: 'new-binary', detail: entry.path });
    }
  }

  if (addedRpcLines > 0) {
    violations.push({ rule: 'new-rpc', detail: `${addedRpcLines} added rpc line(s) in internal.proto` });
  }

  return violations;
}

export interface Verdict {
  level: 'ok' | 'warn' | 'fail';
  reasons: string[];
}

export function verdict(summary: Summary, violations: Violation[], options: { enforce: boolean; exception: boolean }): Verdict {
  const reasons: string[] = [];
  let level: Verdict['level'] = 'ok';

  if (violations.length > 0) {
    reasons.push(`${violations.length} freeze-list violation(s)`);
    level = options.enforce && !options.exception ? 'fail' : 'warn';
  }

  if (summary.netCode > 200 && summary.codeDeleted < summary.codeAdded / 2) {
    reasons.push(`net +${summary.netCode} handwritten code lines with only ${summary.codeDeleted} deleted`);
    if (level === 'ok') level = 'warn';
  }

  if (summary.newFiles > 50) {
    reasons.push(`${summary.newFiles} new files`);
    if (level === 'ok') level = 'warn';
  }

  if (options.exception && violations.length > 0) {
    reasons.push('brake-exception label present: freeze violations do not fail');
  }

  return { level, reasons };
}

function git(args: string[]): string {
  return execFileSync('git', args, { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
}

function parseArgs(argv: string[]): { base: string; enforce: boolean; exception: boolean; summary: boolean } {
  let base = 'origin/main';
  let enforce = false;
  let exception = false;
  let summary = false;

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === '--base') {
      base = argv[index + 1] ?? base;
      index += 1;
    } else if (arg === '--enforce') {
      enforce = true;
    } else if (arg === '--exception') {
      exception = true;
    } else if (arg === '--summary') {
      summary = true;
    }
  }

  return { base, enforce, exception, summary };
}

function readHead(file: string): string {
  try {
    return readFileSync(file, 'utf8').split('\n').slice(0, 6).join('\n');
  } catch {
    return '';
  }
}

function collect(base: string): { entries: DiffEntry[]; addedRpcLines: number; newDirs: Set<string> } {
  const numstat = git(['diff', '--no-renames', '--numstat', `${base}...HEAD`])
    .split('\n')
    .filter((line) => line.trim() !== '');
  const nameStatus = git(['diff', '--no-renames', '--name-status', `${base}...HEAD`])
    .split('\n')
    .filter((line) => line.trim() !== '');

  const statusByPath = new Map<string, 'A' | 'M' | 'D'>();
  for (const line of nameStatus) {
    const [status, file] = line.split('\t');
    if (!file) continue;
    statusByPath.set(file, status === 'A' ? 'A' : status === 'D' ? 'D' : 'M');
  }

  const entries: DiffEntry[] = [];
  for (const line of numstat) {
    const [addedRaw, deletedRaw, file] = line.split('\t');
    if (!file) continue;
    const added = addedRaw === '-' ? 0 : Number(addedRaw);
    const deleted = deletedRaw === '-' ? 0 : Number(deletedRaw);
    const status = statusByPath.get(file) ?? 'M';
    entries.push({
      path: file,
      status,
      added,
      deleted,
      fileClass: classifyFile(file, readHead(file)),
    });
  }

  let addedRpcLines = 0;
  try {
    const protoDiff = git(['diff', '--no-renames', `${base}...HEAD`, '--', 'packages/contracts/proto/internal.proto']);
    for (const line of protoDiff.split('\n')) {
      if (/^\+\s*rpc\s/.test(line)) addedRpcLines += 1;
    }
  } catch {
    addedRpcLines = 0;
  }

  const newDirs = new Set<string>();
  const seenDirs = new Set<string>();
  for (const entry of entries) {
    if (entry.status !== 'A') continue;
    const dir = entry.path.includes('/') ? entry.path.slice(0, entry.path.lastIndexOf('/')) : '';
    if (dir === '' || seenDirs.has(dir)) continue;
    seenDirs.add(dir);
    try {
      if (git(['ls-tree', '-d', '--name-only', base, '--', dir]).trim() === '') newDirs.add(dir);
    } catch {
      // A missing base entry is not evidence of a new directory.
    }
  }

  return { entries, addedRpcLines, newDirs };
}

function formatTable(summary: Summary): string[] {
  const rows: Array<[string, number, number]> = [
    ['handwritten code', summary.codeAdded, summary.codeDeleted],
    ['generated code', summary.generatedAdded, summary.generatedDeleted],
    ['tests', summary.testAdded, summary.testDeleted],
    ['docs', summary.docAdded, summary.docDeleted],
  ];
  const lines = [
    '| class | added | deleted | net |',
    '|---|---:|---:|---:|',
  ];
  for (const [label, added, deleted] of rows) {
    const net = added - deleted;
    lines.push(`| ${label} | ${added} | ${deleted} | ${net >= 0 ? '+' : ''}${net} |`);
  }
  lines.push(`| files | ${summary.newFiles} new | ${summary.deletedFiles} deleted | ${summary.newFiles - summary.deletedFiles >= 0 ? '+' : ''}${summary.newFiles - summary.deletedFiles} |`);
  return lines;
}

function main(): void {
  const options = parseArgs(process.argv.slice(2));
  const { entries, addedRpcLines, newDirs } = collect(options.base);
  const summary = summarize(entries);
  const violations = detectFreezeViolations(entries, addedRpcLines, newDirs);
  const result = verdict(summary, violations, options);

  const lines: string[] = [];
  lines.push(options.summary ? '### Change budget' : 'Change budget');
  lines.push('');
  lines.push(`Base: \`${options.base}\``);
  lines.push('');
  lines.push(...formatTable(summary));

  if (violations.length > 0) {
    lines.push('');
    lines.push('Freeze-list violations:');
    for (const violation of violations) {
      lines.push(`- \`${violation.rule}\`: ${violation.detail}`);
    }
  }

  lines.push('');
  lines.push(`Verdict: **${result.level.toUpperCase()}**`);
  for (const reason of result.reasons) {
    lines.push(`- ${reason}`);
  }
  lines.push('');
  lines.push('Policy: docs/policies/change-budget.md');

  process.stdout.write(`${lines.join('\n')}\n`);

  if (result.level === 'fail') {
    process.exitCode = 1;
  }
}

const invokedDirectly = Boolean(process.argv[1]) && import.meta.url === pathToFileURL(process.argv[1] as string).href;
if (invokedDirectly) {
  main();
}

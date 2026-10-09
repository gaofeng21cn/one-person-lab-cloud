import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { checkPullRequestBody, requiredSections } from '../../tools/check-pr-governance.ts';

const checkerPath = fileURLToPath(new URL('../../tools/check-pr-governance.ts', import.meta.url));
const templatePath = new URL('../../.github/PULL_REQUEST_TEMPLATE.md', import.meta.url);
const canonicalPlanPath = new URL('../../docs/spec/target/checks/development_plan.json', import.meta.url);
const sha = (value: string) => createHash('sha256').update(value).digest('hex');

const fixturePlan = {
  schemaVersion: 1,
  sourceRoots: { cloud: '.', console: 'apps/console-ui', workspace: 'services/workspace' },
  workPackages: [
    { id: 'W16', owners: ['console'], plannedWritePaths: ['apps/console-ui/src/app', 'docs/evidence/source-checks'], startAfter: [], acceptAfter: [] },
    { id: 'W02', owners: ['workspace'], plannedWritePaths: ['services/workspace/internal'], startAfter: [], acceptAfter: [] },
  ],
  executionSlices: [
    { id: 'W16.workspace-ui', workPackage: 'W16', writePaths: ['apps/console-ui/src/app'], startAfter: [], verification: ['npm run typecheck'], inputRefs: ['x'], deliverable: 'd', completionEvidence: 'e', onUnknown: 'u' },
  ],
  parallelPreparation: [],
};
const changedFiles = [
  'apps/console-ui/src/app/use-workspace-models-controller.ts',
  'apps/console-ui/src/app/workspace-models-controller-model.ts',
];
const baseSha = 'b'.repeat(40);

function render(overrides: Record<string, string | null> = {}, extra: string[] = []): string {
  const values: Record<string, string> = {
    'Decision Conclusion': 'Derive the Console model verdict from the Workspace owner readback, not from operation closeout.',
    'Current Problem': 'The Console showed a false unapplied-configuration alert for an attention-required operation.',
    'Business SSOT': 'No business SSOT change: `docs/architecture.md` and `docs/status.md` remain authoritative and unmodified.',
    'Development SSOT': 'No development SSOT change: `AGENTS.md` and `DEV_GUIDE.md` remain authoritative.',
    Baseline: `Base SHA: \`${baseSha}\``,
    Ownership: 'DDD owner: `console`\nPhase: `W16.workspace-ui`',
    'Write Set': changedFiles.map((path) => `- \`${path}\``).join('\n'),
    'Receipt Pipeline': '- receipt: source-check; result: passed; path: `docs/evidence/source-checks/example.json`',
    'Acceptance Criteria': '- [x] The alert follows the owner readback.',
    Verification: '- `node --test tests/ui/workspace-experience-model.test.ts`: 19 tests, 0 failures.',
    Limitations: '- No real owner service or Instance E2E in this slice.',
    'Terminal State': 'Terminal state: source-complete',
    'Merge Danger': 'Merge danger: none',
  };
  for (const [name, value] of Object.entries(overrides)) {
    if (value === null) delete values[name]; else values[name] = value;
  }
  const blocks = requiredSections
    .filter((name) => name in values)
    .map((name) => `## ${name}\n\n${values[name]}\n`);
  return [...blocks, ...extra].join('\n');
}

function reject(body: string, pattern: RegExp, facts: Parameters<typeof checkPullRequestBody>[1] = {}) {
  const result = checkPullRequestBody(body, { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha, ...facts });
  assert.equal(result.ok, false, 'expected governance rejection');
  assert.match(result.errors.join('\n'), pattern);
  return result;
}

test('the pull request template carries every required section exactly once, unedited placeholders cannot pass', () => {
  const template = readFileSync(templatePath, 'utf8');
  for (const name of requiredSections) {
    assert.equal(template.match(new RegExp(`^## ${name}$`, 'mu'))?.length, 1, `template section: ${name}`);
  }
  const result = checkPullRequestBody(template, { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha });
  assert.equal(result.ok, false);
});

test('a complete body with the declared base, owner, phase, write set and source receipt passes', () => {
  const result = checkPullRequestBody(render(), { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha });
  assert.deepEqual(result.errors, []);
  assert.equal(result.ok, true);
  assert.equal(result.claim?.baseSha, baseSha);
  assert.equal(result.claim?.dddOwner, 'console');
  assert.equal(result.claim?.phase, 'W16.workspace-ui');
  assert.deepEqual(result.claim?.writeSet, changedFiles);
  assert.deepEqual(result.claim?.receipts, [{ type: 'source-check', result: 'passed', fields: { path: 'docs/evidence/source-checks/example.json' } }]);
});

test('missing, duplicated and empty required sections are refused', () => {
  reject(render({ 'Merge Danger': null }), /MISSING_SECTION: Merge Danger/);
  reject(render({}, ['## Verification\n\n- `extra`\n']), /DUPLICATE_SECTION: Verification/);
  reject(render({ Limitations: '<!-- only template guidance -->' }), /EMPTY_SECTION: Limitations/);
});

test('the baseline must be one complete SHA and equal the event base when one is supplied', () => {
  reject(render({ Baseline: `Base SHA: \`${baseSha.slice(0, 7)}\`` }), /INVALID_BASE_SHA/);
  reject(render({ Baseline: `Base SHA: \`${'a'.repeat(40)}\`` }), /BASE_SHA_MISMATCH/);
});

test('the phase and DDD owner must resolve inside the development plan and match its ownership', () => {
  reject(render({ Ownership: 'DDD owner: `gateway`\nPhase: `W16.workspace-ui`' }), /OWNER_DOES_NOT_OWN_PHASE/);
  reject(render({ Ownership: 'DDD owner: `console`\nPhase: `W31.publishing`' }), /UNKNOWN_PHASE/);
});

test('the declared write set must be valid relative paths that cover the actual change', () => {
  reject(render({ 'Write Set': '- `/etc/passwd`' }), /INVALID_WRITE_PATH/);
  reject(render({ 'Write Set': '- `apps/console-ui/../../secrets.ts`' }), /INVALID_WRITE_PATH/);
  reject(render({ 'Write Set': `- \`${changedFiles[0]}\`` }), /UNDECLARED_CHANGED_PATH: apps\/console-ui\/src\/app\/workspace-models-controller-model\.ts/);
});

test('receipt entries are typed and refuse unknown types, missing fields and inconsistent identities', () => {
  reject(render({ 'Receipt Pipeline': '- receipt: vibe-check; result: passed; path: `x`' }), /UNKNOWN_RECEIPT_TYPE/);
  reject(render({ 'Receipt Pipeline': '- receipt: development-stage; result: passed; run: `r1`; gate: `acceptance`; attempt: 1' }), /MISSING_RECEIPT_FIELD: development-stage: (path|sha256)/);
  reject(render({ 'Terminal State': 'Terminal state: merge-ready' }), /MERGE_AUTHORIZATION_UNPROVEN/);
  reject(
    render({
      'Terminal State': 'Terminal state: merge-ready',
      'Receipt Pipeline': '- receipt: development-stage; result: pending; path: `runs/r1/receipts/acceptance-1.json`; run: `r1`; gate: `acceptance`; attempt: 1; sha256: `' + 'c'.repeat(64) + '`',
    }),
    /OPEN_RECEIPT_RESULT: development-stage: pending/,
  );
});

test('a development-stage receipt binds its host store path, run, gate and attempt', () => {
  const line = '- receipt: development-stage; result: passed; path: `runs/r1/receipts/other-1.json`; run: `r1`; gate: `acceptance`; attempt: 1; sha256: `' + 'c'.repeat(64) + '`';
  reject(render({ 'Receipt Pipeline': line, 'Terminal State': 'Terminal state: merge-ready' }), /RECEIPT_PATH_MISMATCH/);
});

test('terminal states separate host-accepted merge readiness from source-complete work', () => {
  reject(render({ 'Terminal State': 'Terminal state: done' }), /INVALID_TERMINAL_STATE/);
  reject(
    render({
      'Receipt Pipeline': '- receipt: source-check; result: pending; path: `docs/evidence/source-checks/example.json`',
    }),
    /OPEN_RECEIPT_RESULT: source-check: pending/,
  );
  reject(render({ 'Acceptance Criteria': '- [x] done\n- [ ] still open' }), /ACCEPTANCE_NOT_CLOSED/);
  const blocked = checkPullRequestBody(
    render({ 'Terminal State': 'Terminal state: blocked', 'Acceptance Criteria': '- [x] done\n- [ ] still open', 'Receipt Pipeline': '- receipt: source-check; result: pending; path: `docs/evidence/source-checks/example.json`' }),
    { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha },
  );
  assert.equal(blocked.ok, true, blocked.errors.join('\n'));
  // An author-declared development-stage entry is not evidence: without a
  // trusted host store or a host bootstrap authorization, merge-ready fails.
  const fabricated = checkPullRequestBody(
    render({
      'Terminal State': 'Terminal state: merge-ready',
      'Receipt Pipeline': '- receipt: development-stage; result: passed; path: `runs/r1/receipts/acceptance-2.json`; run: `r1`; gate: `acceptance`; attempt: 2; sha256: `' + 'c'.repeat(64) + '`',
    }),
    { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha },
  );
  assert.equal(fabricated.ok, false);
  assert.match(fabricated.errors.join('\n'), /MERGE_AUTHORIZATION/);
});

test('record compliance and merge authorization are machine-distinct', () => {
  const body = render();
  const record = checkPullRequestBody(body, { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha });
  assert.equal(record.ok, true, record.errors.join('\n'));
  assert.equal(record.authorization?.ok, false);
  assert.equal(record.authorization?.claimed, false);

  const merge = checkPullRequestBody(body, { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha, mode: 'merge' });
  assert.equal(merge.ok, false);
  assert.match(merge.errors.join('\n'), /MERGE_AUTHORIZATION_NOT_CLAIMED/);
});

test('a fabricated development-stage claim cannot authorize merge-ready', () => {
  const body = render({
    'Terminal State': 'Terminal state: merge-ready',
    'Receipt Pipeline': '- receipt: development-stage; result: passed; path: `runs/nonexistent-run/receipts/nonexistent-gate-1.json`; run: `nonexistent-run`; gate: `nonexistent-gate`; attempt: 1; sha256: `' + '0'.repeat(64) + '`',
  });
  const result = checkPullRequestBody(body, { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha, mode: 'merge' });
  assert.equal(result.ok, false);
  assert.match(result.errors.join('\n'), /MERGE_AUTHORIZATION/);
});


test('business and instance receipts cannot replace source-layer evidence or claim merge readiness', () => {
  reject(render({ 'Receipt Pipeline': '- receipt: business; result: passed; authority: `ledger`; ref: `receipt-1`' }), /SOURCE_LAYER_EVIDENCE_REQUIRED/);
  const business = checkPullRequestBody(
    render({
      'Terminal State': 'Terminal state: blocked',
      'Receipt Pipeline': '- receipt: business; result: passed; authority: `ledger`; ref: `receipt-1`\n- receipt: instance; result: pending; ref: `instance-receipt-1`',
    }),
    { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha },
  );
  assert.equal(business.ok, true, business.errors.join('\n'));
  reject(render({ 'Receipt Pipeline': '- receipt: business; result: passed; authority: `ledger`' }), /MISSING_RECEIPT_FIELD: business: ref/);
});

test('a source-check receipt file must exist, stay in the source layer and cover the declared write set', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'opl-governance-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const receiptPath = 'docs/evidence/source-checks/example.json';
  const codeFile = changedFiles[0];
  const codeBytes = 'export const controller = true;\n';
  const receipt = (overrides: Record<string, unknown> = {}) => ({
    schemaVersion: 1,
    receiptType: 'example_source_check',
    evidenceLayer: 'source',
    result: 'pass',
    sourceBaseSha: baseSha,
    writeSet: [...changedFiles, receiptPath],
    verifiedSource: { changedFilesSha256: { [codeFile]: sha(codeBytes) } },
    ...overrides,
  });
  const write = (value: unknown) => {
    mkdirSync(join(directory, dirname(receiptPath)), { recursive: true });
    writeFileSync(join(directory, receiptPath), JSON.stringify(value, null, 2));
    mkdirSync(join(directory, dirname(codeFile)), { recursive: true });
    writeFileSync(join(directory, codeFile), codeBytes);
    for (const path of changedFiles.slice(1)) {
      mkdirSync(join(directory, dirname(path)), { recursive: true });
      writeFileSync(join(directory, path), 'export const model = true;\n');
    }
  };
  const body = render();
  const facts = { plan: fixturePlan, changedPaths: changedFiles, eventBaseSha: baseSha, root: directory };

  write(receipt());
  assert.equal(checkPullRequestBody(body, facts).ok, true);

  write(receipt({ evidenceLayer: 'business' }));
  reject(body, /RECEIPT_LAYER_MISMATCH/, { root: directory, changedPaths: [...changedFiles, receiptPath] });

  write(receipt({ result: 'fail' }));
  reject(body, /RECEIPT_NOT_PASSING/, { root: directory, changedPaths: [...changedFiles, receiptPath] });

  write(receipt({ sourceBaseSha: 'c'.repeat(40) }));
  reject(body, /RECEIPT_BASE_MISMATCH/, { root: directory, changedPaths: [...changedFiles, receiptPath] });

  write(receipt({ writeSet: [...changedFiles, 'services/workspace/internal/undeclared.go'] }));
  reject(body, /RECEIPT_WRITE_SET_MISMATCH/, { root: directory, changedPaths: [...changedFiles, receiptPath] });

  write(receipt({ verifiedSource: { changedFilesSha256: { [codeFile]: sha('a superseded revision') } } }));
  reject(body, /RECEIPT_STALE_HASH/, { root: directory, changedPaths: [...changedFiles, receiptPath] });

  rmSync(join(directory, receiptPath));
  reject(body, /RECEIPT_NOT_FOUND/, { root: directory, changedPaths: [...changedFiles, receiptPath] });
});

test('the phase write scope is authorized with the shared entry policy, not owner membership alone', (t) => {
  const repository = resolve(fileURLToPath(new URL('../..', import.meta.url)));
  const plan: any = JSON.parse(readFileSync(canonicalPlanPath, 'utf8'));

  // Issue: the ownership record can switch to a narrow console phase while the
  // actual change is development tooling.
  const smuggled = checkPullRequestBody(
    render({ Ownership: 'DDD owner: `console`\nPhase: `W16.console-ui-fixes`', 'Write Set': '- `tools/`\n- `.github/`' }),
    { plan, root: repository, changedPaths: ['tools/dev-session.ts', '.github/workflows/pull-request-ci.yml'], eventBaseSha: baseSha },
  );
  assert.equal(smuggled.ok, false);
  assert.match(smuggled.errors.join('\n'), /write scope is outside approved DDD phase: (tools|.github)/);

  const directory = mkdtempSync(join(tmpdir(), 'opl-governance-scope-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const grantedOwnership = 'DDD owner: `console`\nPhase: `W16.console-ui-fixes`';
  const receiptPath = 'docs/evidence/source-checks/scope-example.json';
  const codeFile = 'apps/console-ui/src/app/workspace-models-controller-model.ts';
  const codeBytes = 'export const controller = true;\n';
  mkdirSync(join(directory, 'apps/console-ui/src/app'), { recursive: true });
  mkdirSync(join(directory, 'tests/ui'), { recursive: true });
  mkdirSync(join(directory, 'docs/evidence/source-checks'), { recursive: true });
  writeFileSync(join(directory, codeFile), codeBytes);
  writeFileSync(join(directory, 'tests/ui/workspace-experience-model.test.ts'), 'export const test = true;\n');
  writeFileSync(join(directory, receiptPath), JSON.stringify({
    schemaVersion: 1, receiptType: 'scope_example', evidenceLayer: 'source', result: 'pass', sourceBaseSha: baseSha,
    writeSet: [codeFile, 'tests/ui/workspace-experience-model.test.ts', receiptPath],
    verifiedSource: { changedFilesSha256: { [codeFile]: sha(codeBytes) } },
  }));
  const grantedBody = render({
    Ownership: grantedOwnership,
    'Write Set': [
      '- `apps/console-ui/src/app/`',
      '- `tests/ui/workspace-experience-model.test.ts`',
      `- \`${receiptPath}\``,
    ].join('\n'),
    'Receipt Pipeline': `- receipt: source-check; result: passed; path: \`${receiptPath}\``,
  });
  const granted = checkPullRequestBody(grantedBody, {
    plan,
    root: directory,
    changedPaths: [codeFile, 'tests/ui/workspace-experience-model.test.ts', receiptPath],
    eventBaseSha: baseSha,
  });
  assert.deepEqual(granted.errors, []);

  const outsideGrant = checkPullRequestBody(
    render({ Ownership: grantedOwnership, 'Write Set': '- `tests/ui/`' }),
    { plan, root: directory, changedPaths: ['tests/ui/workspace-budget-controller-model.test.ts'], eventBaseSha: baseSha },
  );
  assert.equal(outsideGrant.ok, false);
  assert.match(outsideGrant.errors.join('\n'), /write scope is outside approved DDD phase: tests/);

  // A body may declare the exact granted test file but not a business-owned path.
  const borrowedBusinessPath = checkPullRequestBody(
    render({ Ownership: grantedOwnership, 'Write Set': '- `services/workspace/internal/launch.go`' }),
    { plan, root: directory, changedPaths: ['services/workspace/internal/launch.go'], eventBaseSha: baseSha },
  );
  assert.equal(borrowedBusinessPath.ok, false);
  assert.match(borrowedBusinessPath.errors.join('\n'), /write scope is outside approved DDD phase|INVALID_OWNER/);
});

test('the canonical plan resolves the console surface fix and development governance slices to their owners', () => {
  const plan: any = JSON.parse(readFileSync(canonicalPlanPath, 'utf8'));
  const consoleBody = render({
    Ownership: 'DDD owner: `console`\nPhase: `W16.console-ui-fixes`',
    'Write Set': '- `apps/console-ui/src/pages/`\n- `tests/ui/`',
  });
  const consoleResult = checkPullRequestBody(consoleBody, {
    plan,
    changedPaths: ['apps/console-ui/src/pages/CustomerPages.tsx', 'tests/ui/workspace-experience-model.test.ts'],
    eventBaseSha: baseSha,
  });
  assert.deepEqual(consoleResult.errors, []);

  const governanceBody = render({
    Ownership: 'DDD owner: `cloud`\nPhase: `W27.development-governance`',
    'Write Set': '- `.github/PULL_REQUEST_TEMPLATE.md`\n- `tools/check-pr-governance.ts`',
  });
  const governanceResult = checkPullRequestBody(governanceBody, {
    plan,
    changedPaths: ['.github/PULL_REQUEST_TEMPLATE.md', 'tools/check-pr-governance.ts'],
    eventBaseSha: baseSha,
  });
  assert.deepEqual(governanceResult.errors, []);
});

test('the bootstrap path enforces record compliance, while a base that has the checker still enforces merge', (t) => {
  // Physical condition only: the pull request's base revision predates the
  // checker. Simulated with a real worktree-shaped directory, not a runner.
  const directory = mkdtempSync(join(tmpdir(), 'opl-governance-bootstrap-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const repository = join(directory, 'repo');
  const bootstrapBase = join(directory, 'base-without-checker');
  const presentBase = join(directory, 'base-with-checker');
  mkdirSync(join(bootstrapBase, 'docs'), { recursive: true });
  mkdirSync(join(presentBase, 'tools'), { recursive: true });
  writeFileSync(join(presentBase, 'tools/check-pr-governance.ts'), readFileSync(checkerPath));
  // A minimal checked revision: plan, implementation files and a passing
  // source-check receipt, all committed so the change scope is empty.
  const planPath = 'docs/spec/target/checks/development_plan.json';
  const receiptPath = 'docs/evidence/source-checks/example.json';
  const codeBytes = 'export const controller = true;\n';
  mkdirSync(join(repository, 'apps/console-ui/src/app'), { recursive: true });
  writeFileSync(join(repository, changedFiles[0]), codeBytes);
  writeFileSync(join(repository, changedFiles[1]), 'export const model = true;\n');
  mkdirSync(join(repository, dirname(receiptPath)), { recursive: true });
  mkdirSync(join(repository, dirname(planPath)), { recursive: true });
  const gitFixture = (...args: string[]) => execFileSync('git', ['-C', repository, ...args], { encoding: 'utf8' });
  gitFixture('init', '-q');
  // The body declares its base only after the fixture commit exists.
  const bodyPath = join(directory, 'body.md');
  const writeBody = (base: string) => writeFileSync(bodyPath, render({
    Baseline: `Base SHA: \`${base}\``,
    'Write Set': ['- `' + changedFiles[0] + '`', '- `' + changedFiles[1] + '`', '- `' + receiptPath + '`'].join('\n'),
  }));
  writeBody('0'.repeat(40));
  writeFileSync(join(repository, planPath), JSON.stringify(fixturePlan));
  writeFileSync(join(repository, receiptPath), JSON.stringify({
    schemaVersion: 1, receiptType: 'example_source_check', evidenceLayer: 'source', result: 'pass',
    sourceBaseSha: '0'.repeat(40), writeSet: [...changedFiles, receiptPath],
    verifiedSource: { changedFilesSha256: { [changedFiles[0]]: sha(codeBytes) } },
  }));
  gitFixture('add', '.');
  gitFixture('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'checked revision fixture');
  const fixtureBase = gitFixture('rev-parse', 'HEAD').trim();
  writeBody(fixtureBase);
  writeFileSync(join(repository, receiptPath), JSON.stringify({
    schemaVersion: 1, receiptType: 'example_source_check', evidenceLayer: 'source', result: 'pass',
    sourceBaseSha: fixtureBase, writeSet: [...changedFiles, receiptPath],
    verifiedSource: { changedFilesSha256: { [changedFiles[0]]: sha(codeBytes) } },
  })); // file content is not part of the body check; the receipt file itself stays committed-eligible
  gitFixture('add', '.'); gitFixture('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'receipt binds the fixture base');

  // Base lacks the checker: merge mode downgrades to record compliance and the
  // validate step must not fail on the missing host acceptance.
  const bootstrap = spawnSync('node', [
    checkerPath, '--body-file', bodyPath, '--root', repository,
    '--base', fixtureBase, '--mode', 'merge', '--bootstrap-base', bootstrapBase,
  ], { encoding: 'utf8' });
  assert.equal(bootstrap.status, 0, bootstrap.stdout + bootstrap.stderr);
  const bootstrapResult = JSON.parse(bootstrap.stdout);
  assert.equal(bootstrapResult.mode, 'merge');
  assert.equal(bootstrapResult.effectiveMode, 'record');
  assert.equal(bootstrapResult.bootstrap, true);
  assert.equal(bootstrapResult.ok, true, JSON.stringify(bootstrapResult.errors));

  // A base that physically has the checker keeps the merge enforcement: the
  // same source-complete body fails without a verified host acceptance.
  const enforced = spawnSync('node', [
    checkerPath, '--body-file', bodyPath, '--root', repository,
    '--base', fixtureBase, '--mode', 'merge', '--bootstrap-base', presentBase,
  ], { encoding: 'utf8' });
  assert.equal(enforced.status, 1, enforced.stdout + enforced.stderr);
  const enforcedResult = JSON.parse(enforced.stdout);
  assert.equal(enforcedResult.effectiveMode, 'merge');
  assert.equal(enforcedResult.bootstrap, false);
  assert.match(enforcedResult.errors.join('\n'), /MERGE_AUTHORIZATION_NOT_CLAIMED/);

  // A missing bootstrap-base directory is not a downgrade condition.
  const absent = spawnSync('node', [
    checkerPath, '--body-file', bodyPath, '--root', repository,
    '--base', fixtureBase, '--mode', 'merge', '--bootstrap-base', join(directory, 'does-not-exist'),
  ], { encoding: 'utf8' });
  assert.equal(absent.status, 1, absent.stdout + absent.stderr);
});

test('the real command entry reads the body, the plan and the git change scope', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'opl-governance-cli-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const git = (...args: string[]) => execFileSync('git', ['-C', directory, ...args], { encoding: 'utf8' });
  mkdirSync(join(directory, 'apps/console-ui/src/app'), { recursive: true });
  mkdirSync(join(directory, 'docs/spec/target/checks'), { recursive: true });
  writeFileSync(join(directory, 'apps/console-ui/src/app/input.ts'), 'export const answer = 42;\n');
  writeFileSync(join(directory, 'docs/spec/target/checks/development_plan.json'), JSON.stringify(fixturePlan));
  git('init', '-q');
  git('add', '.');
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'baseline');
  const baseline = git('rev-parse', 'HEAD').trim();
  writeFileSync(join(directory, 'apps/console-ui/src/app/workspace-models-controller-model.ts'), 'export const verdict = "applied";\n');
  const receiptPath = 'docs/evidence/source-checks/example.json';
  mkdirSync(join(directory, dirname(receiptPath)), { recursive: true });
  writeFileSync(join(directory, receiptPath), JSON.stringify({
    schemaVersion: 1,
    receiptType: 'example_source_check',
    evidenceLayer: 'source',
    result: 'pass',
    sourceBaseSha: baseline,
    writeSet: ['apps/console-ui/src/app/workspace-models-controller-model.ts', receiptPath],
    verifiedSource: { changedFilesSha256: { 'apps/console-ui/src/app/workspace-models-controller-model.ts': sha('export const verdict = "applied";\n') } },
  }));
  const outside = mkdtempSync(join(tmpdir(), 'opl-governance-bodies-'));
  t.after(() => rmSync(outside, { recursive: true, force: true }));
  const bodyPath = join(outside, 'body.md');
  writeFileSync(
    bodyPath,
    render(
      { Baseline: `Base SHA: \`${baseline}\``, 'Write Set': '- `apps/console-ui/src/app/workspace-models-controller-model.ts`\n- `' + receiptPath + '`' },
    ),
  );
  git('add', 'apps/console-ui/src/app/workspace-models-controller-model.ts');
  git('add', receiptPath);
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'change');
  const head = git('rev-parse', 'HEAD').trim();

  const run = spawnSync(
    'node',
    [checkerPath, '--body-file', bodyPath, '--root', directory, '--base', baseline, '--head', head],
    { encoding: 'utf8' },
  );
  assert.equal(run.status, 0, run.stdout + run.stderr);
  assert.match(run.stdout, /"ok": true/);

  const abbreviated = spawnSync(
    'node',
    [checkerPath, '--body-file', bodyPath, '--root', directory, '--base', baseline.slice(0, 7), '--head', head],
    { encoding: 'utf8' },
  );
  assert.equal(abbreviated.status, 1);
  assert.match(abbreviated.stdout + abbreviated.stderr, /abbreviated base SHA refused/);

  const asRef = spawnSync(
    'node',
    [checkerPath, '--body-file', bodyPath, '--root', directory, '--base', head, '--head', head],
    { encoding: 'utf8' },
  );
  assert.equal(asRef.status, 1);
  assert.match(asRef.stdout + asRef.stderr, /BASE_SHA_MISMATCH/);

  const incompletePath = join(outside, 'incomplete.md');
  writeFileSync(incompletePath, render({ Baseline: `Base SHA: \`${baseline}\``, 'Write Set': '- `apps/console-ui/src/app/input.ts`' }));
  assert.match(readFileSync(bodyPath, 'utf8'), /docs\/evidence\/source-checks\/example\.json/);
  const incomplete = spawnSync(
    'node',
    [checkerPath, '--body-file', incompletePath, '--root', directory, '--base', baseline, '--head', head],
    { encoding: 'utf8' },
  );
  assert.equal(incomplete.status, 1);
  assert.match(incomplete.stdout + incomplete.stderr, /UNDECLARED_CHANGED_PATH/);
});

test('the package entry exposes the governance check and keeps it inside the development gates', () => {
  const manifest = JSON.parse(readFileSync(new URL('../../package.json', import.meta.url), 'utf8'));
  assert.equal(manifest.scripts['check:pr-body'], 'node tools/check-pr-governance.ts');
  for (const target of ['tests/tools/check-pr-governance.test.ts', 'tests/tools/check-pr-merge-authorization.test.ts']) {
    assert.ok(manifest.scripts['test:development-gates'].includes(target), target);
  }
  assert.match(manifest.scripts['typecheck:development-tools'], /tools\/check-pr-governance\.ts/);

  // CI runs the checker from the trusted base revision and enforces merge
  // authorization inside the existing validate job, not through `needs`.
  const workflow = readFileSync(new URL('../../.github/workflows/pull-request-ci.yml', import.meta.url), 'utf8');
  assert.match(workflow, /types: \[opened, synchronize, reopened, edited\]/);
  assert.match(workflow, /--mode record/);
  assert.match(workflow, /--mode merge/);
  assert.match(workflow, /--authority-env OPL_DEVELOPMENT_AUTHORITY_PUBLIC_KEY/);
  assert.match(workflow, /vars\.OPL_DEVELOPMENT_AUTHORITY_PUBLIC_KEY/);
  assert.equal(/pull_request_target/.test(workflow), false, 'no pull_request_target execution');
  assert.equal(/dependabot\[bot\]/.test(workflow), false, 'no actor-based governance exemption');
  // The checker runs from the trusted base revision with the head as data.
  assert.match(workflow, /worktree add --detach --force "\$base_dir" "\$PR_BASE_SHA"/);
  assert.match(workflow, /Governance bootstrap/);
  // Physical bootstrap condition only: the validate merge step passes the base
  // worktree so the checker itself downgrades to record enforcement when the
  // base revision has no checker file; a base that carries the checker keeps
  // merge enforcement. No actor or user condition takes part.
  assert.match(workflow, /--bootstrap-base "\$base_dir"/);
  assert.match(workflow, /bootstrap-checked via head copy; merge authorization is not enforced remotely for this first landing and IS enforced from the next pull request/);
  assert.equal(/github\.actor/.test(workflow), false, 'no actor condition in the governance chain');
  assert.equal(existsSync(new URL('../../tools/ci/governance-check.sh', import.meta.url)), false);
});

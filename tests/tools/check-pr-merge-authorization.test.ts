import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import test from 'node:test';
import * as development from '../../tools/dev-session.ts';
import { checkPullRequestBody, requiredSections } from '../../tools/check-pr-governance.ts';

const planPath = 'docs/spec/target/checks/development_plan.json';
const sha256 = (value: string | Buffer) => createHash('sha256').update(value).digest('hex');
function canonical(value: any): string {
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.keys(value).sort().map((key) => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}';
  return JSON.stringify(value);
}
const receiptDigest = (record: unknown) => sha256(canonical(record));
function put(root: string, path: string, content: string) {
  mkdirSync(resolve(root, path, '..'), { recursive: true });
  writeFileSync(join(root, path), content);
}
/** A source-check receipt is the current in-repo record; the host store stays separate. */
function writeSourceCheck(root: string, path: string, base: string, ownerPath: string) {
  put(root, path, JSON.stringify({
    schemaVersion: 1, receiptType: 'owner_acceptance_source_check', evidenceLayer: 'source', result: 'pass',
    sourceBaseSha: base, writeSet: [ownerPath, path],
    verifiedSource: { changedFilesSha256: { [ownerPath]: sha256(readFileSync(join(root, ownerPath))) } },
    execution: { command: 'node --test --test-reporter=tap owner/acceptance.test.mjs', exitCode: 0, tests: 1, failed: 0, skipped: 0, todo: 0, outputSha256: sha256('tap output') },
  }));
  return path;
}

function fixture(t: any) {
  const directory = mkdtempSync(join(tmpdir(), 'opl-merge-auth-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const root = join(directory, 'repo'); const store = join(directory, 'host'); mkdirSync(root);
  for (const file of ['AGENTS.md', 'DEV_GUIDE.md', 'docs/status.md', 'docs/roadmap.md']) put(root, file, `current ${file}\n`);
  put(root, 'owner/input.ts', 'export const answer = 42;\n');
  put(root, 'owner/acceptance.test.mjs', "import test from 'node:test';import assert from 'node:assert/strict';import {answer} from './input.ts';test('approved behavior',()=>assert.equal(answer,42));\n");
  const plan = { schemaVersion: 1, sourceRoots: { cloud: '.', owner: 'owner' },
    workPackages: [{ id: 'W01', title: 'Owner behavior', owners: ['owner'], startAfter: [], acceptAfter: [], existingReadPaths: ['owner/input.ts'], plannedWritePaths: ['owner'], verification: ['owner acceptance'], acceptance: ['observable answer'] }],
    executionSlices: [], parallelPreparation: [] };
  put(root, planPath, JSON.stringify(plan));
  const git = (...args: string[]) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8' });
  git('init', '-q'); git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'approved baseline');
  const approval = { schemaVersion: 1, runId: 'owner-run', baseSha: git('rev-parse', 'HEAD').trim(), planPath,
    selection: { collection: 'workPackages', id: 'W01' }, owner: 'owner', readPaths: ['owner/'], writePaths: ['owner/input.ts', 'owner/new.ts', 'owner/acceptance.test.mjs', 'docs/evidence/source-checks/'],
    gates: [{ id: 'acceptance', kind: 'node', inputs: ['owner/'], targets: ['owner/acceptance.test.mjs'], needs: [] }], requires: {} };
  return { root, store, git, plan, approval };
}

function render(values: Record<string, string>): string {
  const defaults: Record<string, string> = {
    'Decision Conclusion': 'Merge the admitted owner change.', 'Current Problem': 'The owner behavior needs the admitted change.',
    'Business SSOT': 'No business SSOT change: `docs/architecture.md` remains authoritative.', 'Development SSOT': 'No development SSOT change: `AGENTS.md` remains authoritative.',
    Baseline: '', Ownership: 'DDD owner: `owner`\nPhase: `W01`', 'Write Set': '- `owner/input.ts`', 'Receipt Pipeline': '',
    'Acceptance Criteria': '- [x] The owner behavior is verified.', Verification: '- `node --test`: executed.', Limitations: '- Source layer only.',
    'Terminal State': 'Terminal state: merge-ready', 'Merge Danger': 'Merge danger: none',
  };
  const merged = { ...defaults, ...values };
  return requiredSections.map((name) => `## ${name}\n\n${merged[name]}\n`).join('\n');
}

test('a real host store receipt authorizes merge-ready and forged, foreign or tampered claims are refused', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// admitted implementation change\n');
  const verified = await session.verifyGate('acceptance') as any;
  assert.equal(verified.result, 'passed', JSON.stringify(verified));
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const recordPath = join(store, receiptPath);
  const record = JSON.parse(readFileSync(recordPath, 'utf8'));
  const digest = receiptDigest(record);
  const stageEntry = `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\``;
  const base = approval.baseSha;
  const head = git('rev-parse', 'HEAD').trim();
  const sourcePath = writeSourceCheck(root, 'docs/evidence/source-checks/owner-acceptance.json', base, 'owner/input.ts');
  const mergeBody = (pipeline: string) => render({
    'Terminal State': 'Terminal state: merge-ready', Baseline: `Base SHA: \`${base}\``,
    'Write Set': `- \`owner/input.ts\`\n- \`${sourcePath}\``,
    'Receipt Pipeline': `${pipeline}
- receipt: source-check; result: passed; path: \`${sourcePath}\``,
  });
  const facts = { plan, root, changedPaths: ['owner/input.ts', sourcePath], eventBaseSha: base, headSha: head, mode: 'merge', hostStore: store } as const;

  const authorized = checkPullRequestBody(mergeBody(stageEntry), facts);
  assert.equal(authorized.ok, true, authorized.errors.join('\n'));
  assert.equal(authorized.authorization?.basis, 'host-store');
  assert.equal(authorized.authorization?.ok, true);

  // A run without any host record in the store.
  const ghost = checkPullRequestBody(
    mergeBody(`- receipt: development-stage; result: passed; path: \`runs/ghost-run/receipts/acceptance-1.json\`; run: \`ghost-run\`; gate: \`acceptance\`; attempt: 1; sha256: \`${'0'.repeat(64)}\``),
    facts,
  );
  assert.equal(ghost.ok, false);
  assert.match(ghost.errors.join('\n'), /EVIDENCE_INVALID/);

  // The host record of another run is not this run's evidence.
  development.approveRun(root, store, { ...approval, runId: 'second-run' });
  mkdirSync(join(store, 'runs', 'second-run', 'receipts'), { recursive: true });
  writeFileSync(join(store, 'runs', 'second-run', 'receipts', 'acceptance-1.json'), readFileSync(recordPath));
  const foreign = checkPullRequestBody(
    mergeBody(`- receipt: development-stage; result: passed; path: \`runs/second-run/receipts/acceptance-1.json\`; run: \`second-run\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\``),
    facts,
  );
  assert.equal(foreign.ok, false);
  assert.match(foreign.errors.join('\n'), /EVIDENCE_INVALID/);

  // Tampering with a host record changes its digest, so the declared digest no
  // longer matches; a rewritten record is not evidence.
  const tampered = { ...record, verification: { ...record.verification, outputSha256: sha256('tampered') } };
  writeFileSync(recordPath, JSON.stringify(tampered));
  const rewritten = checkPullRequestBody(mergeBody(stageEntry), facts);
  assert.equal(rewritten.ok, false);
  assert.match(rewritten.errors.join('\n'), /EVIDENCE_INVALID/);
  writeFileSync(recordPath, JSON.stringify(record));

  // A locally created record cannot substitute a store outside the host boundary:
  // a repository clone of the same bytes is author-controlled data.
  const cloneRoot = mkdtempSync(join(tmpdir(), 'opl-merge-auth-clone-'));
  t.after(() => rmSync(cloneRoot, { recursive: true, force: true }));
  mkdirSync(join(cloneRoot, 'docs/evidence/source-checks'), { recursive: true });
  const clonedStore = join(cloneRoot, 'docs');
  const cloned = checkPullRequestBody(mergeBody(stageEntry), { ...facts, hostStore: clonedStore });
  assert.equal(cloned.ok, false);
  assert.match(cloned.errors.join('\n'), /HOST_STORE_INSIDE_REPOSITORY|EVIDENCE_INVALID/);

  // The record still no longer matches the checked revision's declared inputs.
  writeFileSync(join(root, 'owner/input.ts'), 'export const answer = 43;\n');
  const stale = checkPullRequestBody(mergeBody(stageEntry), facts);
  assert.equal(stale.ok, false);
  assert.match(stale.errors.join('\n'), /EVIDENCE_STALE_INPUT/);
});

test('a host-exported source-check receipt is exactly what the record check accepts', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// executed change\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed', 'the real node gate executed');
  const exported = development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance');
  const base = approval.baseSha;
  const head = git('rev-parse', 'HEAD').trim();
  const body = render({
    Baseline: `Base SHA: \`${base}\``,
    'Write Set': `- \`owner/input.ts\`\n- \`${exported.path}\``,
    'Receipt Pipeline': `- receipt: source-check; result: passed; path: \`${exported.path}\``,
  });
  const changedPaths = ['owner/input.ts', exported.path];
  const accepted = checkPullRequestBody(body, { plan, root, changedPaths, eventBaseSha: base, headSha: head, mode: 'record' });
  assert.equal(accepted.ok, true, accepted.errors.join('\n'));

  // One current source-check authority per pull request: mutual exemption between
  // two receipts is refused instead of creating a cycle.
  const second = exported.path.replace('-acceptance-1.json', '-acceptance-2.json');
  put(root, second, readFileSync(join(root, exported.path), 'utf8'));
  const doubled = checkPullRequestBody(
    render({
      Baseline: `Base SHA: \`${base}\``,
      'Write Set': `- \`owner/input.ts\`\n- \`${exported.path}\`\n- \`${second}\``,
      'Receipt Pipeline': `- receipt: source-check; result: passed; path: \`${exported.path}\`\n- receipt: source-check; result: passed; path: \`${second}\``,
    }),
    { plan, root, changedPaths: [...changedPaths, second], eventBaseSha: base, headSha: head, mode: 'record' },
  );
  assert.equal(doubled.ok, false);
  assert.match(doubled.errors.join('\n'), /RECEIPT_MULTIPLE_CURRENT/);
});

test('the host approval record binds the declared base and a store inside the repository is refused', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context(); await session.verifyGate('acceptance');
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const record = JSON.parse(readFileSync(join(store, receiptPath), 'utf8'));
  const stageEntry = `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${receiptDigest(record)}\``;

  // A "trusted store" located inside the repository would be author-controlled data.
  const inside = checkPullRequestBody(
    render({ 'Terminal State': 'Terminal state: source-complete', Baseline: `Base SHA: \`${approval.baseSha}\``, 'Receipt Pipeline': stageEntry }),
    { plan, root, changedPaths: [], eventBaseSha: approval.baseSha, headSha: git('rev-parse', 'HEAD').trim(), mode: 'merge', hostStore: join(root, 'docs') },
  );
  assert.equal(inside.ok, false);
  assert.match(inside.errors.join('\n'), /HOST_STORE_INSIDE_REPOSITORY|EVIDENCE_INVALID/);

  // An ancestor directory of the repository is equally author-controlled and
  // must not become a trusted store through a prefix comparison.
  const ancestor = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${approval.baseSha}\``, 'Receipt Pipeline': stageEntry }),
    { plan, root, changedPaths: [], eventBaseSha: approval.baseSha, headSha: git('rev-parse', 'HEAD').trim(), mode: 'merge', hostStore: resolve(root, '..') },
  );
  assert.equal(ancestor.ok, false);
  assert.match(ancestor.errors.join('\n'), /HOST_STORE_INSIDE_REPOSITORY/);

  // The host approval binds one base; a body declaring another base is refused.
  const approvalPath = join(store, 'runs', approval.runId, 'approval.json');
  const approvalRecord = JSON.parse(readFileSync(approvalPath, 'utf8'));
  approvalRecord.baseSha = 'c'.repeat(40);
  writeFileSync(approvalPath, JSON.stringify(approvalRecord));
  const mismatch = checkPullRequestBody(
    render({ 'Terminal State': 'Terminal state: source-complete', Baseline: `Base SHA: \`${approval.baseSha}\``, 'Receipt Pipeline': stageEntry }),
    { plan, root, changedPaths: [], eventBaseSha: approval.baseSha, headSha: git('rev-parse', 'HEAD').trim(), mode: 'merge', hostStore: store },
  );
  assert.equal(mismatch.ok, false);
  assert.match(mismatch.errors.join('\n'), /EVIDENCE_BASE_MISMATCH/);
});

test('only the host store authorizes a development-stage record; no repo-local proof or proof-only commit exists', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// candidate change\n');
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'implementation candidate');
  session.context();
  const verified = await session.verifyGate('acceptance') as any;
  assert.equal(verified.result, 'passed', JSON.stringify(verified));
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const record = JSON.parse(readFileSync(join(store, receiptPath), 'utf8'));
  const candidate = git('rev-parse', 'HEAD').trim();
  assert.equal(record.sourceSha, candidate);

  const head = git('rev-parse', 'HEAD').trim();
  const base = approval.baseSha;
  const body = render({
    Baseline: `Base SHA: \`${base}\``,
    'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${receiptDigest(record)}\``,
  });
  const facts = { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: head, mode: 'merge' as const };

  // Without the host store, merge-ready is refused instead of downgraded.
  const unproven = checkPullRequestBody(body, facts);
  assert.equal(unproven.ok, false);
  assert.match(unproven.errors.join('\n'), /MERGE_AUTHORIZATION_UNPROVEN/);

  // A `proof:` field is a retired receipt field and is refused.
  const withProof = checkPullRequestBody(
    render({ 'Terminal State': 'Terminal state: source-complete', Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${receiptDigest(record)}\`; proof: \`docs/evidence/development-stage/owner-run-acceptance-1.json\`` }),
    { ...facts, hostStore: store },
  );
  assert.equal(withProof.ok, false);
  assert.match(withProof.errors.join('\n'), /UNKNOWN_RECEIPT_FIELD: development-stage: proof/);

  // An in-repo JSON copy is never read as evidence.
  const inRepo = 'docs/evidence/development-stage/owner-run-acceptance-1.json';
  mkdirSync(join(root, dirname(inRepo)), { recursive: true });
  writeFileSync(join(root, inRepo), JSON.stringify({ schemaVersion: 1, kind: 'opl.development.stage.proof.v1', approval: JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'approval.json'), 'utf8')), receipt: record }, null, 2));
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'repo-local record copy');
  const withLocalCopy = checkPullRequestBody(body, { ...facts, headSha: git('rev-parse', 'HEAD').trim() });
  assert.equal(withLocalCopy.ok, false);
  assert.match(withLocalCopy.errors.join('\n'), /MERGE_AUTHORIZATION_UNPROVEN/);
});

test('merge authorization fails closed without a host store and refuses evidence-layer mixing', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context(); await session.verifyGate('acceptance');
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const record = JSON.parse(readFileSync(join(store, receiptPath), 'utf8'));
  const base = approval.baseSha;
  const head = git('rev-parse', 'HEAD').trim();
  const stageEntry = `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${receiptDigest(record)}\``;

  // No host store: merge-ready is refused, not skipped and not green.
  const unproven = checkPullRequestBody(
    render({ 'Terminal State': 'Terminal state: source-complete', Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': stageEntry }),
    { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: head, mode: 'merge' },
  );
  assert.equal(unproven.ok, false);
  assert.match(unproven.errors.join('\n'), /MERGE_AUTHORIZATION_UNPROVEN/);

  // Evidence-layer mixing: a business-layer record in the store is refused even
  // though the store is trusted for source receipts.
  const mixedPath = `runs/${approval.runId}/receipts/acceptance-2.json`;
  writeFileSync(join(store, mixedPath), JSON.stringify({ ...record, attempt: 2, evidenceLayer: 'business' }));
  const mixed = checkPullRequestBody(
    render({ 'Terminal State': 'Terminal state: source-complete', Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`${mixedPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 2; sha256: \`${receiptDigest({ ...record, attempt: 2, evidenceLayer: 'business' })}\`` }),
    { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: head, mode: 'merge', hostStore: store },
  );
  assert.equal(mixed.ok, false);
  assert.match(mixed.errors.join('\n'), /EVIDENCE_INVALID/);

  const businessOnly = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${base}\``, 'Terminal State': 'Terminal state: merge-ready', 'Receipt Pipeline': '- receipt: business; result: passed; authority: `ledger`; ref: `receipt-1`' }),
    { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: head, mode: 'merge' },
  );
  assert.equal(businessOnly.ok, false);
  assert.match(businessOnly.errors.join('\n'), /SOURCE_LAYER_EVIDENCE_REQUIRED|MERGE_AUTHORIZATION/);
});

import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { createServer } from 'node:http';
import { accessSync, chmodSync, constants, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, watch, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import * as development from '../../tools/dev-session.ts';
import { checkPullRequestBody, requiredSections } from '../../tools/check-pr-governance.ts';

const sha = (value: string) => createHash('sha256').update(value).digest('hex');
const planPath = 'docs/spec/target/checks/development_plan.json';
function put(root: string, path: string, content: string) {
  mkdirSync(resolve(root, path, '..'), { recursive: true });
  writeFileSync(join(root, path), content);
}
function fixture(t: any) {
  const directory = mkdtempSync(join(tmpdir(), 'opl-entry-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const root = join(directory, 'repo'); const store = join(directory, 'host'); mkdirSync(root);
  for (const file of ['AGENTS.md', 'DEV_GUIDE.md', 'docs/status.md', 'docs/roadmap.md']) put(root, file, `current ${file}\n`);
  put(root, 'owner/input.ts', 'export const answer = 42;\n');
  put(root, 'other/input.ts', 'outside scope\n');
  put(root, 'owner/acceptance.test.mjs', "import test from 'node:test';import assert from 'node:assert/strict';import {answer} from './input.ts';test('approved behavior',()=>assert.equal(answer,42));\n");
  put(root, planPath, JSON.stringify({ schemaVersion: 1, sourceRoots: { cloud: '.', owner: 'owner', other: 'other' },
    workPackages: [{ id: 'W01', title: 'Owner behavior', owners: ['owner'], startAfter: [], acceptAfter: [], existingReadPaths: ['owner/input.ts'], plannedWritePaths: ['owner'], verification: ['owner acceptance'], acceptance: ['observable answer'] },
      { id: 'W02', title: 'Other behavior', owners: ['other'], startAfter: [], acceptAfter: [], existingReadPaths: ['other/input.ts'], plannedWritePaths: ['other'], verification: ['other acceptance'], acceptance: ['observable other'] }],
    executionSlices: [], parallelPreparation: [] }));
  const git = (...args: string[]) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8' });
  git('init', '-q'); git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'approved baseline');
  const approval = { schemaVersion: 1, runId: 'owner-run', baseSha: git('rev-parse', 'HEAD').trim(), planPath,
    selection: { collection: 'workPackages', id: 'W01' }, owner: 'owner', readPaths: ['owner/'], writePaths: ['owner/input.ts', 'owner/new.ts', 'owner/acceptance.test.mjs'],
    gates: [{ id: 'acceptance', kind: 'node', inputs: ['owner/'], targets: ['owner/acceptance.test.mjs'], needs: [] }], requires: {} };
  return { root, store, git, approval };
}

function canonicalFixture(t: any) {
  const current = fixture(t);
  // Exercise the canonical plan unchanged, not a parallel fixture definition.
  put(current.root, planPath, readFileSync(new URL('../../docs/spec/target/checks/development_plan.json', import.meta.url), 'utf8'));
  put(current.root, 'packages/contracts/go/admission.go', 'package contracts\n');
  put(current.root, 'services/serve/migrations/admission.sql', 'SELECT 1;\n');
  put(current.root, 'services/serve/cmd/server/main.go', 'package main\n');
  current.git('add', '.'); current.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'canonical phase plan');
  return { ...current, approval: { ...current.approval, baseSha: current.git('rev-parse', 'HEAD').trim() } };
}

test('canonical W01 admission resolves host plan wildcards into exact worker permissions', async t => {
  const { root, store, approval } = canonicalFixture(t);
  const exact = { ...approval, owner: 'cloud', selection: { collection: 'executionSlices', id: 'W01.application-contracts' },
    readPaths: ['packages/contracts/go/admission.go', 'services/serve/migrations/admission.sql'],
    writePaths: ['packages/contracts/go/admission.go', 'services/serve/migrations/admission.sql'] };
  development.approveRun(root, store, exact);
  const session = new development.DevelopmentSession(root, store, exact.runId); session.context();
  for (const path of exact.writePaths) {
    const file = session.read(path); session.write(path, file.sha256, file.content + '// exact admitted write\n');
  }
  assert.throws(() => session.read('services/serve/cmd/server/main.go'), /SCOPE_DENIED/);
  assert.throws(() => session.read('services/*/migrations/admission.sql'), /invalid relative path/);
  assert.throws(() => session.write('services/*/migrations/admission.sql', 'absent', 'denied'), /invalid relative path/);
  for (const field of ['readPaths', 'writePaths'] as const) {
    assert.throws(() => development.approveRun(root, store, { ...exact, runId: `wildcard-${field}`, [field]: ['services/*/migrations/'] }), /invalid relative path/);
  }
  assert.throws(() => development.approveRun(root, store, { ...exact, runId: 'wrong-pattern-match', writePaths: ['services/serve/cmd/server/main.go'] }), /outside approved DDD/);
  assert.throws(() => development.approveRun(root, store, { ...exact, runId: 'missing-wildcard-owner', writePaths: ['services/nonexistent/migrations/admission.sql'] }), /outside approved DDD/);
});

test('canonical parallelPreparation admits only owners of its existing execution slices', t => {
  const { root, store, approval } = canonicalFixture(t);
  const scopes = [
    { window: 'resources', owner: 'fabric', path: 'services/fabric/admission.go' },
    { window: 'artifacts', owner: 'capability', path: 'services/capability/admission.go' },
    { window: 'business', owner: 'workspace', path: 'services/workspace/admission.go' },
    { window: 'delivery', owner: 'serve', path: 'services/serve/admission.go' },
    { window: 'artifacts', owner: 'console', path: 'apps/console-ui/src/pages/PublisherPage.tsx' },
  ];
  const coauthorRuns: string[] = [];
  for (const { window, owner, path } of scopes) {
    put(root, path, 'admitted owner input\n');
    const scoped = { ...approval, runId: `prepare-${owner}`, owner,
      selection: { collection: 'parallelPreparation', id: window }, readPaths: [path], writePaths: [path], coauthorRuns: [...coauthorRuns] };
    assert.throws(() => development.approveRun(root, store, { ...scoped, runId: `cloud-${owner}`, owner: 'cloud' }), /DDD owner does not own phase record/);
    development.approveRun(root, store, scoped);
    const session = new development.DevelopmentSession(root, store, scoped.runId);
    assert.equal(session.context().owner, owner);
    const file = session.read(path); session.write(path, file.sha256, file.content + '// preparation before terminal acceptance\n');
    assert.throws(() => session.read('other/input.ts'), /SCOPE_DENIED/);
    coauthorRuns.push(scoped.runId);
  }
});

test('startAfter requires the complete predecessor run even when a gateId is supplied', async t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.sourceRoots.terminal = 'terminal';
  plan.workPackages[0].acceptAfter = ['W03'];
  plan.workPackages[1].startAfter = ['W01'];
  plan.workPackages.push({ id: 'W03', owners: ['terminal'], plannedWritePaths: ['terminal'], startAfter: [], acceptAfter: [] });
  put(root, planPath, JSON.stringify(plan));
  for (const path of ['owner/second.test.mjs', 'other/acceptance.test.mjs', 'terminal/acceptance.test.mjs']) {
    put(root, path, "import test from 'node:test';test('declared acceptance',()=>{});\n");
  }
  put(root, 'terminal/input.ts', 'terminal input\n');
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'whole-phase prerequisites');
  const baseSha = git('rev-parse', 'HEAD').trim();
  development.approveRun(root, store, { ...approval, baseSha,
    gates: [...approval.gates, { id: 'second', kind: 'node', inputs: ['owner/second.test.mjs'], targets: ['owner/second.test.mjs'], needs: [] }],
    requires: { W03: { runId: 'terminal-run' } } });
  development.approveRun(root, store, { ...approval, baseSha, runId: 'terminal-run', owner: 'terminal',
    selection: { collection: 'workPackages', id: 'W03' }, readPaths: ['terminal/'], writePaths: ['terminal/input.ts'],
    gates: [{ id: 'terminal', kind: 'node', inputs: ['terminal/'], targets: ['terminal/acceptance.test.mjs'], needs: [] }] });
  development.approveRun(root, store, { ...approval, baseSha, runId: 'consumer-run', owner: 'other',
    selection: { collection: 'workPackages', id: 'W02' }, readPaths: ['other/'], writePaths: ['other/input.ts'],
    gates: [{ id: 'consumer', kind: 'node', inputs: ['other/'], targets: ['other/acceptance.test.mjs'], needs: [] }],
    requires: { W01: { runId: approval.runId, gateId: 'acceptance' } } });
  const producer = new development.DevelopmentSession(root, store, approval.runId);
  const consumer = new development.DevelopmentSession(root, store, 'consumer-run');
  const terminal = new development.DevelopmentSession(root, store, 'terminal-run');
  producer.context(); terminal.context();
  assert.equal((await producer.verifyGate('acceptance')).stages[0].state, 'passed');
  assert.equal(consumer.context().result, 'blocked', 'one selected passed gate is not a completed predecessor phase');
  await assert.rejects(consumer.verifyGate('consumer'), /DEPENDENCY_BLOCKED/);
  assert.throws(() => consumer.write('other/input.ts', sha('outside scope\n'), 'premature'), /DEPENDENCY_BLOCKED/);
  await producer.verifyGate('second');
  assert.equal(producer.status().result, 'blocked', 'independent gates may pass before terminal acceptance');
  assert.equal(consumer.status().result, 'blocked', 'all gates passed cannot hide unresolved acceptAfter');
  await terminal.verifyGate('terminal');
  assert.equal(producer.status().result, 'passed');
  assert.equal(consumer.status().result, 'ready');
  assert.equal((await consumer.verifyGate('consumer')).result, 'passed');
});

test('acceptAfter receipt changes invalidate transitive consumers without restarting independent stages', async t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.workPackages[1].acceptAfter = ['W01'];
  const added = [
    { id: 'W03', owner: 'middle', acceptAfter: ['W02'], startAfter: [] },
    { id: 'W04', owner: 'consumer', acceptAfter: [], startAfter: ['W03'] },
    { id: 'W05', owner: 'independent', acceptAfter: [], startAfter: [] },
  ];
  for (const { id, owner, acceptAfter, startAfter } of added) {
    plan.sourceRoots[owner] = owner;
    plan.workPackages.push({ id, owners: [owner], plannedWritePaths: [owner], startAfter, acceptAfter });
    put(root, `${owner}/input.ts`, `${owner} input\n`);
  }
  for (const owner of ['other', ...added.map(record => record.owner)]) {
    put(root, `${owner}/acceptance.test.mjs`, "import test from 'node:test';test('independent stage acceptance',()=>{});\n");
  }
  put(root, planPath, JSON.stringify(plan));
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'transitive terminal chain');
  const baseSha = git('rev-parse', 'HEAD').trim();
  development.approveRun(root, store, { ...approval, baseSha });
  const records = [
    { id: 'W02', owner: 'other', runId: 'other-run', requires: { W01: { runId: approval.runId } } },
    { id: 'W03', owner: 'middle', runId: 'middle-run', requires: { W02: { runId: 'other-run' } } },
    { id: 'W04', owner: 'consumer', runId: 'consumer-run', requires: { W03: { runId: 'middle-run' } } },
    { id: 'W05', owner: 'independent', runId: 'independent-run', requires: {} },
  ];
  for (const { id, owner, runId, requires } of records) development.approveRun(root, store, { ...approval, baseSha, runId, owner,
    selection: { collection: 'workPackages', id }, readPaths: [`${owner}/`], writePaths: [`${owner}/input.ts`],
    coauthorRuns: owner === 'independent' ? [approval.runId] : [],
    gates: [{ id: 'acceptance', kind: 'node', inputs: [`${owner}/`], targets: [`${owner}/acceptance.test.mjs`], needs: [] }], requires });
  const producer = new development.DevelopmentSession(root, store, approval.runId);
  const other = new development.DevelopmentSession(root, store, 'other-run');
  const middle = new development.DevelopmentSession(root, store, 'middle-run');
  const consumer = new development.DevelopmentSession(root, store, 'consumer-run');
  const independent = new development.DevelopmentSession(root, store, 'independent-run');
  for (const session of [producer, other, middle, consumer, independent]) session.context();
  // Terminal prerequisites are not stage prerequisites: local owner work can proceed.
  for (const session of [other, middle]) {
    assert.equal((await session.verifyGate('acceptance')).stages[0].state, 'passed');
    assert.equal(session.status().result, 'blocked');
  }
  assert.equal((await independent.verifyGate('acceptance')).result, 'passed');
  assert.equal(consumer.status().result, 'blocked');
  await producer.verifyGate('acceptance');
  assert.equal((await consumer.verifyGate('acceptance')).result, 'passed');
  const bound = (session: development.DevelopmentSession) => ({ runId: session.approval.runId, gateId: 'acceptance', receiptHash: session.status().stages[0].receiptHash! });
  const originalDependencies = [bound(middle), bound(other), bound(producer)];
  const oldIndependentHash = bound(independent).receiptHash;
  const receiptDirectory = join(store, 'runs', 'consumer-run', 'receipts');
  const originalBytes = readFileSync(join(receiptDirectory, 'acceptance-1.json'));
  for (const session of [producer, other, middle, consumer, independent]) {
    put(store, `runs/${session.approval.runId}/receipts/business-owner-receipt.json`, 'poison: not development evidence');
  }
  const input = producer.read('owner/input.ts');
  producer.write(input.path, input.sha256, input.content + '// exact producer inputs changed\n');
  assert.equal(consumer.status().result, 'blocked');
  assert.equal((await producer.verifyGate('acceptance')).result, 'passed');
  assert.equal(consumer.status().result, 'ready', 'replacement terminal evidence must invalidate the old downstream receipt');
  assert.equal(new development.DevelopmentSession(root, store, 'consumer-run').context().result, 'ready');
  for (const session of [other, middle, independent]) {
    assert.equal(session.status().result, 'passed');
    const reused = await session.verifyGate('acceptance');
    assert.ok('reused' in reused); assert.equal(reused.reused, true);
    assert.deepEqual(readdirSync(join(store, 'runs', session.approval.runId, 'receipts')).sort(), ['acceptance-1.json', 'business-owner-receipt.json']);
  }
  assert.equal(bound(independent).receiptHash, oldIndependentHash);
  assert.deepEqual(JSON.parse(originalBytes.toString()).dependencies, originalDependencies);
  const accepted = await consumer.verifyGate('acceptance');
  assert.equal(accepted.result, 'passed');
  assert.deepEqual(readFileSync(join(receiptDirectory, 'acceptance-1.json')), originalBytes);
  const current = JSON.parse(readFileSync(join(receiptDirectory, 'acceptance-2.json'), 'utf8'));
  assert.equal(current.evidenceLayer, 'source');
  assert.deepEqual(current.dependencies, [bound(middle), bound(other), bound(producer)]);
  assert.notEqual(current.dependencies[2].receiptHash, originalDependencies[2].receiptHash);
});

test('cyclic acceptAfter evidence fails closed rather than overflowing terminal traversal', async t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.workPackages[0].acceptAfter = ['W02']; plan.workPackages[1].acceptAfter = ['W01'];
  put(root, planPath, JSON.stringify(plan));
  put(root, 'other/acceptance.test.mjs', "import test from 'node:test';test('independent stage',()=>{});\n");
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'cyclic terminal prerequisites');
  const baseSha = git('rev-parse', 'HEAD').trim();
  development.approveRun(root, store, { ...approval, baseSha, requires: { W02: { runId: 'other-run' } } });
  development.approveRun(root, store, { ...approval, baseSha, runId: 'other-run', owner: 'other',
    selection: { collection: 'workPackages', id: 'W02' }, readPaths: ['other/'], writePaths: ['other/input.ts'],
    gates: [{ id: 'acceptance', kind: 'node', inputs: ['other/'], targets: ['other/acceptance.test.mjs'], needs: [] }],
    requires: { W01: { runId: approval.runId } } });
  const owner = new development.DevelopmentSession(root, store, approval.runId);
  const other = new development.DevelopmentSession(root, store, 'other-run');
  owner.context(); other.context();
  assert.equal((await owner.verifyGate('acceptance')).stages[0].state, 'passed');
  await assert.rejects(other.verifyGate('acceptance'), /EVIDENCE_INVALID: cyclic evidence dependencies/);
  for (const session of [owner, other]) assert.throws(() => session.context(), /EVIDENCE_INVALID: cyclic evidence dependencies/);
});

test('every fresh worker must receive current phase-linked context before any operation', t => {
  const { root, store, approval } = fixture(t);
  (development as any).approveRun(root, store, approval);
  for (let index = 0; index < 2; index++) {
    const session = new development.DevelopmentSession(root, store, approval.runId);
    assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
    const current = session.context();
    assert.equal(current.owner, 'owner');
    assert.equal(current.selection.id, 'W01');
    assert.equal(current.nextGate, 'acceptance');
    assert.equal(session.read('owner/input.ts').content, 'export const answer = 42;\n');
  }
});

test('worker cannot call shell, completion or publication, even after admission', async t => {
  const { root, store, approval } = fixture(t);
  (development as any).approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId); session.context();
  for (const tool of ['shell', 'exec', 'complete', 'dev_complete', 'dev_publish']) {
    await assert.rejects((development as any).dispatchTool(session, tool, {}), /unknown tool/);
  }
  await assert.rejects((development as any).dispatchTool(session, 'dev_read', { path: 'owner/input.ts', recursive: true }), /unknown field/);
});

test('actual acceptance closes source work, survives restart, and never writes product status', async t => {
  const { root, store, approval } = fixture(t);
  (development as any).approveRun(root, store, approval);
  const before = ['docs/status.md', 'docs/roadmap.md'].map(p => readFileSync(join(root, p), 'utf8'));
  const first = new development.DevelopmentSession(root, store, approval.runId); first.context();
  const result = await (first as any).verifyGate('acceptance');
  assert.equal(result.result, 'passed', JSON.stringify(result));
  const restarted = new development.DevelopmentSession(root, store, approval.runId);
  assert.throws(() => restarted.read('owner/input.ts'), /CONTEXT_REQUIRED/);
  const current = restarted.context();
  assert.equal(current.result, 'passed');
  assert.equal(current.nextGate, null);
  assert.deepEqual(['docs/status.md', 'docs/roadmap.md'].map(p => readFileSync(join(root, p), 'utf8')), before);
  assert.equal(readdirSync(join(store, 'runs', approval.runId, 'receipts')).filter(p => p.endsWith('.json')).length, 1);
});

test('public progress and verification never read poisoned unrelated gate or business receipt bodies', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const first = new development.DevelopmentSession(root, store, approval.runId);
  first.context(); assert.equal((await first.verifyGate('acceptance')).result, 'passed');
  const directory = join(store, 'runs', approval.runId, 'receipts');
  const accepted = readFileSync(join(directory, 'acceptance-1.json'));
  // Invalid JSON proves these bodies are not parsed. A directory makes any
  // attempted regular-file read fail even before parsing or signature checks.
  for (const path of ['unrelated-1.json', 'acceptance-detail-1.json', 'acceptance-01.json']) {
    writeFileSync(join(directory, path), 'poison: must not be read');
  }
  mkdirSync(join(directory, 'business-owner-receipt.json'));
  const restarted = new development.DevelopmentSession(root, store, approval.runId);
  const context = await development.dispatchTool(restarted, 'dev_context', {});
  assert.ok(context && 'result' in context); assert.equal(context.result, 'passed');
  const progress = await development.dispatchTool(restarted, 'dev_status', {});
  assert.ok(progress && 'stages' in progress); assert.equal(progress.stages[0].state, 'passed');
  const reused = await development.dispatchTool(restarted, 'dev_verify', { gateId: 'acceptance' });
  assert.ok(reused && 'reused' in reused); assert.equal(reused.reused, true);
  const input = restarted.read('owner/input.ts');
  restarted.write(input.path, input.sha256, input.content + '// actual input changed\n');
  const invalidated = await development.dispatchTool(restarted, 'dev_status', {});
  assert.ok(invalidated && 'stages' in invalidated); assert.equal(invalidated.stages[0].state, 'pending');
  const verified = await development.dispatchTool(restarted, 'dev_verify', { gateId: 'acceptance' });
  assert.ok(verified && 'result' in verified); assert.equal(verified.result, 'passed');
  assert.deepEqual(readFileSync(join(directory, 'acceptance-1.json')), accepted);
  assert.ok(existsSync(join(directory, 'acceptance-2.json')));
});

test('public admission rejects misfiled host records, sequence gaps and filename identity mismatches', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, { ...approval, gates: [...approval.gates, { ...approval.gates[0], id: 'other-gate' }] });
  const session = new development.DevelopmentSession(root, store, approval.runId); session.context();
  assert.equal((await session.verifyGate('acceptance')).stages[0].state, 'passed');
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// second exact input set\n');
  assert.equal((await session.verifyGate('acceptance')).stages[0].state, 'passed');
  assert.equal((await session.verifyGate('other-gate')).result, 'passed');
  const directory = join(store, 'runs', approval.runId, 'receipts');
  const first = readFileSync(join(directory, 'acceptance-1.json'));
  const second = readFileSync(join(directory, 'acceptance-2.json'));
  for (const corruption of ['identity mismatch', 'gap', 'swapped attempts', 'foreign gate'] as const) {
    await t.test(corruption, async () => {
      try {
        if (corruption === 'identity mismatch') {
          const record = JSON.parse(first.toString()); record.runId = 'another-run';
          writeFileSync(join(directory, 'acceptance-1.json'), JSON.stringify(record));
        } else if (corruption === 'gap') rmSync(join(directory, 'acceptance-1.json'));
        else if (corruption === 'swapped attempts') {
          writeFileSync(join(directory, 'acceptance-1.json'), second);
          writeFileSync(join(directory, 'acceptance-2.json'), first);
        } else writeFileSync(join(directory, 'acceptance-1.json'), readFileSync(join(directory, 'other-gate-1.json')));
        const restarted = new development.DevelopmentSession(root, store, approval.runId);
        await assert.rejects(development.dispatchTool(restarted, 'dev_context', {}), /EVIDENCE_INVALID/);
      } finally {
        writeFileSync(join(directory, 'acceptance-1.json'), first);
        writeFileSync(join(directory, 'acceptance-2.json'), second);
      }
      assert.equal(new development.DevelopmentSession(root, store, approval.runId).context().result, 'passed');
    });
  }
});

test('owner scope rejects root scans, traversal, symlinks, unknown fields and self-expansion', async t => {
  const { root, store, approval } = fixture(t);
  assert.throws(() => (development as any).approveRun(root, store, { ...approval, runId: 'bad-run', writePaths: ['other/'] }), /outside approved DDD/);
  assert.throws(() => (development as any).approveRun(root, store, { ...approval, runId: 'bad-run', arbitraryCommand: 'true' }), /unknown field/);
  (development as any).approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId); session.context();
  for (const path of ['.', '..', '/', 'owner/../other', '.git/config', '.runtime/key']) assert.throws(() => session.search([path], 'answer'));
  assert.throws(() => session.read('other/input.ts'), /SCOPE_DENIED/);
  symlinkSync(join(root, 'other/input.ts'), join(root, 'owner/new.ts'));
  assert.throws(() => session.read('owner/new.ts'), /symlink refused/);
  rmSync(join(root, 'owner/new.ts'));
  session.context();
  const acceptance = session.read('owner/acceptance.test.mjs');
  session.write('owner/acceptance.test.mjs', acceptance.sha256, acceptance.content.replace('answer,42', 'answer,41'));
  assert.equal(session.read('owner/acceptance.test.mjs').content.includes('answer,41'), true);
  assert.throws(() => (development as any).approveRun(root, store, approval), /EEXIST/);
});

test('context loads the development contract, validates the current baseline scope and exposes all ready gates', t => {
  const { root, store, approval, git } = fixture(t);
  (development as any).approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  const current = session.context();
  assert.deepEqual(current.context.map((item: any) => item.path), ['AGENTS.md', 'DEV_GUIDE.md', 'docs/status.md', 'docs/roadmap.md']);
  assert.deepEqual(current.readyTasks, [{ gateId: 'acceptance', owner: 'owner', action: 'implement-or-verify' }]);
  put(root, 'outside.ts', 'out of scope\n'); git('add', 'outside.ts');
  assert.throws(() => new development.DevelopmentSession(root, store, 'owner-run').context(), /WRITE_SCOPE_DENIED/);
});

test('context exposes every independent ready gate and blocks only its declared dependents', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, { ...approval, gates: [approval.gates[0],
    { ...approval.gates[0], id: 'independent' }, { ...approval.gates[0], id: 'dependent', needs: ['acceptance'] }] });
  const session = new development.DevelopmentSession(root, store, approval.runId);
  assert.deepEqual(session.context().readyTasks.map(task => task.gateId), ['acceptance', 'independent']);
  await session.verifyGate('acceptance');
  assert.deepEqual(session.status().readyTasks.map(task => task.gateId), ['independent', 'dependent']);
  const resumed = new development.DevelopmentSession(root, store, approval.runId).context();
  assert.equal(resumed.stages[0].state, 'passed');
  assert.deepEqual(resumed.readyTasks.map(task => task.gateId), ['independent', 'dependent']);
});

test('failed context refresh revokes the previous admission before any further file operation', t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  put(root, 'outside.ts', 'unowned input\n');
  assert.throws(() => session.context(), /WRITE_SCOPE_DENIED/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
  assert.throws(() => session.write('owner/new.ts', 'absent', 'not admitted'), /CONTEXT_REQUIRED/);
});

test('an invalid host receipt record during context refresh revokes an existing admission', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context(); await session.verifyGate('acceptance');
  const file = join(store, 'runs', approval.runId, 'receipts', 'acceptance-1.json');
  const record = JSON.parse(readFileSync(file, 'utf8')); record.attempt = 7;
  writeFileSync(file, JSON.stringify(record));
  assert.throws(() => session.context(), /EVIDENCE_INVALID/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
});

test('malformed context requests revoke the existing admission before argument rejection', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  for (const input of [{ extra: true }, null, []]) {
    session.context();
    await assert.rejects(development.dispatchTool(session, 'dev_context', input));
    assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
  }
});

test('new out-of-scope changes revoke admission at the operation entry without a context request', t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  put(root, 'outside.ts', 'not admitted\n');
  assert.throws(() => session.read('owner/input.ts'), /WRITE_SCOPE_DENIED/);
  assert.throws(() => session.status(), /CONTEXT_REQUIRED/);
});

test('signed read scope does not authorize read-only input mutations', t => {
  const { root, store, approval, git } = fixture(t);
  put(root, 'owner/read-only.ts', 'read-only baseline\n');
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'read-only baseline');
  development.approveRun(root, store, { ...approval, baseSha: git('rev-parse', 'HEAD').trim() });
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  assert.equal(session.read('owner/read-only.ts').content, 'read-only baseline\n');
  put(root, 'owner/read-only.ts', 'unauthorized mutation\n');
  assert.throws(() => session.context(), /WRITE_SCOPE_DENIED: owner\/read-only.ts/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
});

test('a signed acceptance target is not a write grant', t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, { ...approval, writePaths: ['owner/input.ts'] });
  put(root, 'owner/acceptance.test.mjs', 'unauthorized test replacement\n');
  assert.throws(() => new development.DevelopmentSession(root, store, approval.runId).context(),
    /WRITE_SCOPE_DENIED: owner\/acceptance.test.mjs/);
});

test('only explicitly referenced coauthors can contribute concurrent workspace changes', t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, { ...approval, runId: 'other-run', owner: 'other',
    selection: { collection: 'workPackages', id: 'W02' }, readPaths: ['other/'], writePaths: ['other/input.ts'],
    gates: [{ ...approval.gates[0], inputs: ['other/input.ts'] }] });
  development.approveRun(root, store, approval);
  put(root, 'other/input.ts', 'signed but not referenced\n');
  assert.throws(() => new development.DevelopmentSession(root, store, approval.runId).context(),
    /WRITE_SCOPE_DENIED: other\/input.ts/);
  development.approveRun(root, store, { ...approval, runId: 'shared-run', coauthorRuns: ['other-run'] });
  const session = new development.DevelopmentSession(root, store, 'shared-run');
  assert.deepEqual(session.context().changedPaths, ['other/input.ts']);
  assert.deepEqual((development as any).verifyWriteScope(root, store, 'shared-run').changedPaths, ['other/input.ts']);
  assert.throws(() => session.write('other/input.ts', sha('signed but not referenced\n'), 'owner escape'), /SCOPE_DENIED/);
  // Unrelated host records and business receipts must not be parsed.
  put(store, 'runs/unrelated/approval.json', 'poison: not referenced');
  put(store, 'runs/other-run/receipts/business.json', 'poison: not development evidence');
  assert.equal(session.context().result, 'ready');
  const file = join(store, 'runs', 'other-run', 'approval.json');
  const record = JSON.parse(readFileSync(file, 'utf8')); record.extraField = 'not an approved host field';
  writeFileSync(file, JSON.stringify(record));
  assert.throws(() => session.context(), /EVIDENCE_INVALID/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
});

test('coauthor run declarations are strict host fields', t => {
  const { root, store, approval } = fixture(t);
  for (const [runId, coauthorRuns, expected] of [
    ['bad-coauthors', 'other-run', /invalid coauthor runs/],
    ['duplicate-coauthors', ['other-run', 'other-run'], /duplicate coauthor runs/],
    ['self-coauthor', ['self-coauthor'], /self coauthor refused/],
  ] as const) {
    assert.throws(() => development.approveRun(root, store, { ...approval, runId, coauthorRuns } as any), expected);
  }
});

test('a plan directory alone cannot authorize a coauthor change', t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  put(root, 'other/input.ts', 'unassigned coauthor change\n');
  const session = new development.DevelopmentSession(root, store, approval.runId);
  assert.throws(() => session.context(), /WRITE_SCOPE_DENIED: other\/input.ts/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
});

test('current evidence or HEAD drift requires re-admission; CAS protects concurrent edits', t => {
  const { root, store, approval, git } = fixture(t); (development as any).approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId); session.context();
  writeFileSync(join(root, 'docs/status.md'), 'new evidence');
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_STALE/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/); session.context();
  const old = session.read('owner/input.ts'); writeFileSync(join(root, 'owner/input.ts'), 'concurrent');
  assert.throws(() => session.write('owner/input.ts', old.sha256, 'lost update'), /WRITE_CONFLICT/);
  const current = session.read('owner/input.ts'); session.write('owner/input.ts', current.sha256, 'accepted');
  assert.equal(session.read('owner/input.ts').content, 'accepted');
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'unrelated revision');
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_STALE/);
});

test('downstream admission is blocked by an unverified real predecessor, not by unrelated owners', t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8')); plan.workPackages[1].startAfter = ['W01'];
  writeFileSync(join(root, planPath), JSON.stringify(plan));
  put(root, 'other/acceptance.test.mjs', "import test from 'node:test';test('other behavior',()=>{});\n");
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'approved handoff');
  const baseSha = git('rev-parse', 'HEAD').trim();
  (development as any).approveRun(root, store, { ...approval, baseSha });
  const downstream = { ...approval, runId: 'other-run', baseSha, selection: { collection: 'workPackages', id: 'W02' }, owner: 'other',
    readPaths: ['other/'], writePaths: ['other/input.ts'], gates: [{ id: 'other-acceptance', kind: 'node', inputs: ['other/'], targets: ['other/acceptance.test.mjs'], needs: [] }],
    requires: { W01: { runId: approval.runId } } };
  (development as any).approveRun(root, store, downstream);
  const session = new development.DevelopmentSession(root, store, downstream.runId);
  const current = session.context();
  assert.equal(current.result, 'blocked');
  assert.equal(current.blockers[0].dependency, 'W01');
  assert.equal(current.blockers[0].owner, 'owner');
  assert.throws(() => session.write('other/input.ts', sha('outside scope\n'), 'premature'), /DEPENDENCY_BLOCKED/);
});

test('dedicated worker refuses self-declared completion and advertises no native tools', async t => {
  const { root, store, approval } = fixture(t); (development as any).approveRun(root, store, approval);
  let requests = 0;
  const server = createServer(async (req, res) => {
    let bytes = ''; for await (const chunk of req) bytes += chunk;
    const input = JSON.parse(bytes); requests++;
    assert.match(input.messages[0].content, /current docs\/status.md/);
    assert.deepEqual(input.tools.map((tool: any) => tool.function.name).sort(), ['dev_context', 'dev_read', 'dev_search', 'dev_status', 'dev_verify', 'dev_write']);
    res.setHeader('content-type', 'application/json');
    res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'I declare this work complete.' } }] }));
  });
  await new Promise<void>(done => server.listen(0, '127.0.0.1', done));
  t.after(() => new Promise<void>(done => server.close(() => done())));
  const address = server.address() as any;
  const session = new development.DevelopmentSession(root, store, approval.runId);
  const result = await (development as any).runRestrictedWorker(session, { endpoint: `http://127.0.0.1:${address.port}/v1/chat/completions`, model: 'fixture', maxTurns: 2 });
  assert.equal(result.result, 'blocked');
  assert.equal(result.reason, 'TURN_BUDGET_EXHAUSTED');
  assert.equal(requests, 2);
  assert.equal(session.status().stages[0].state, 'pending');
});

test('the host refreshes worker admission and dynamic progress after other-owner HEAD and status changes', async t => {
  const { root, store, approval, git } = fixture(t);
  put(root, 'other/acceptance.test.mjs', "import test from 'node:test';test('second stage',()=>{});\n");
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'host-declared second stage baseline');
  development.approveRun(root, store, { ...approval, baseSha: git('rev-parse', 'HEAD').trim(), gates: [...approval.gates,
    { id: 'second', kind: 'node', inputs: ['other/'], targets: ['other/acceptance.test.mjs'], needs: [] }] });
  const admissions: any[] = [];
  const server = createServer(async (req, res) => {
    let bytes = ''; for await (const chunk of req) bytes += chunk;
    const input = JSON.parse(bytes);
    admissions.push(JSON.parse(input.messages[0].content.split('Current admission:\n')[1]));
    const turn = admissions.length;
    if (turn === 2) {
      put(root, 'docs/status.md', 'new owner-authoritative evidence\n');
      put(root, 'docs/roadmap.md', 'updated owner gaps\n');
      git('add', 'other/', 'docs/status.md', 'docs/roadmap.md');
      git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'other-owner progress');
    }
    const gateId = turn === 1 || turn === 4 ? 'acceptance' : 'second';
    const message = turn === 2 ? { role: 'assistant', content: 'Continue with current evidence.' } :
      { role: 'assistant', content: null, tool_calls: [{ id: `verify-${turn}`, type: 'function',
        function: { name: 'dev_verify', arguments: JSON.stringify({ gateId }) } }] };
    res.setHeader('content-type', 'application/json');
    res.end(JSON.stringify({ choices: [{ message }] }));
  });
  await new Promise<void>(done => server.listen(0, '127.0.0.1', done));
  t.after(() => new Promise<void>(done => server.close(() => done())));
  const address = server.address() as any;
  const options = { endpoint: `http://127.0.0.1:${address.port}/v1/chat/completions`, model: 'first-model', maxTurns: 1 };
  const partial = await development.runRestrictedWorker(new development.DevelopmentSession(root, store, approval.runId), options);
  assert.equal(partial.result, 'blocked');
  assert.ok('reason' in partial);
  assert.equal(partial.reason, 'TURN_BUDGET_EXHAUSTED');
  assert.deepEqual(partial.stages.map(stage => stage.state), ['passed', 'pending']);
  const result = await development.runRestrictedWorker(new development.DevelopmentSession(root, store, approval.runId),
    { ...options, model: 'second-model', maxTurns: 2 });
  assert.equal(result.result, 'passed', JSON.stringify(result));
  assert.equal(admissions.length, 3);
  assert.equal(admissions[1].stages[0].state, 'passed');
  assert.equal(admissions[1].nextGate, 'second');
  assert.equal(admissions[2].observedHead, git('rev-parse', 'HEAD').trim());
  assert.equal(admissions[2].context.find((item: any) => item.path === 'docs/status.md').content, 'new owner-authoritative evidence\n');
  assert.equal(admissions[2].context.find((item: any) => item.path === 'docs/roadmap.md').content, 'updated owner gaps\n');
  assert.equal(admissions[2].stages[0].receiptHash, admissions[1].stages[0].receiptHash);
  const receipts = readdirSync(join(store, 'runs', approval.runId, 'receipts')).sort();
  assert.deepEqual(receipts, ['acceptance-1.json', 'second-1.json']);
  const resumed = await development.runRestrictedWorker(new development.DevelopmentSession(root, store, approval.runId), { ...options, model: 'replacement-model' });
  assert.equal(resumed.result, 'passed');
  assert.equal(resumed.turns, 0);
  assert.equal(admissions.length, 3);
  assert.deepEqual(readdirSync(join(store, 'runs', approval.runId, 'receipts')).sort(), receipts);
  const changed = new development.DevelopmentSession(root, store, approval.runId); changed.context();
  const input = changed.read('owner/input.ts');
  changed.write(input.path, input.sha256, input.content + '// changed only the first stage\n');
  const invalidated = await development.dispatchTool(changed, 'dev_status', {});
  assert.ok(invalidated && 'stages' in invalidated);
  assert.deepEqual(invalidated.stages.map(stage => stage.state), ['pending', 'passed']);
  const repaired = await development.runRestrictedWorker(new development.DevelopmentSession(root, store, approval.runId),
    { ...options, model: 'third-model' });
  assert.equal(repaired.result, 'passed');
  assert.deepEqual(admissions[3].stages.map((stage: any) => stage.state), ['pending', 'passed']);
  assert.equal(admissions[3].stages[1].receiptHash, result.stages[1].receiptHash);
  assert.deepEqual(readdirSync(join(store, 'runs', approval.runId, 'receipts')).sort(), ['acceptance-1.json', 'acceptance-2.json', 'second-1.json']);
  assert.equal(readFileSync(join(root, 'docs/status.md'), 'utf8'), 'new owner-authoritative evidence\n');
});

test('worker admission refresh errors terminate before another model request, including terminal readback', async t => {
  for (const maxTurns of [1, 2]) await t.test(`turn budget ${maxTurns}`, async t => {
    const { root, store, approval } = fixture(t);
    development.approveRun(root, store, approval);
    let requests = 0;
    const server = createServer(async (req, res) => {
      for await (const _ of req) { /* Consume the model request. */ }
      requests++;
      put(root, 'docs/status.md', 'changed current evidence\n');
      rmSync(join(root, 'docs/roadmap.md'));
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'Continue using old context.' } }] }));
    });
    await new Promise<void>(done => server.listen(0, '127.0.0.1', done));
    t.after(() => new Promise<void>(done => server.close(() => done())));
    const address = server.address() as any;
    const session = new development.DevelopmentSession(root, store, approval.runId);
    await assert.rejects(development.runRestrictedWorker(session, {
      endpoint: `http://127.0.0.1:${address.port}/v1/chat/completions`, model: 'fixture', maxTurns,
    }), (error: any) => error.code === 'ENOENT' && error.path.endsWith('/docs/roadmap.md'));
    assert.equal(requests, 1);
    assert.equal(existsSync(join(store, 'runs', approval.runId, 'receipts')), false);
    await assert.rejects(development.dispatchTool(session, 'dev_read', { path: 'owner/input.ts' }), /CONTEXT_REQUIRED/);
  });
});

test('developmentPlan is a first-class acceptance runner and an unknown kind is refused', t => {
  const { root, store, approval } = fixture(t);
  (development as any).approveRun(root, store, {
    ...approval, runId: 'plan-run',
    gates: [{ id: 'plan', kind: 'developmentPlan', inputs: ['owner/'], needs: [] }]
  });
  const rejected = { ...approval, runId: 'bad-kind-run',
    gates: [{ id: 'plan', kind: 'shell', inputs: ['owner/'], needs: [] }] };
  assert.throws(() => (development as any).approveRun(root, store, rejected), /unknown acceptance runner/);
});

test('real stdio entry requires context on every process and refuses forged acceptance results', async t => {
  const { root, store, approval } = fixture(t); (development as any).approveRun(root, store, approval);
  for (let index = 0; index < 2; index++) {
    const child = spawn(process.execPath, [resolve('tools/dev-session.ts'), 'serve', store, approval.runId], { cwd: root, stdio: ['pipe', 'pipe', 'pipe'] });
    t.after(() => child.kill()); let stdout = ''; let stderr = '';
    child.stdout.on('data', b => { stdout += b; }); child.stderr.on('data', b => { stderr += b; });
    const calls = [
      { id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18' } },
      { id: 2, method: 'tools/list' },
      { id: 3, method: 'tools/call', params: { name: 'dev_read', arguments: { path: 'owner/input.ts' } } },
      { id: 4, method: 'tools/call', params: { name: 'dev_context', arguments: {} } },
      { id: 5, method: 'tools/call', params: { name: 'dev_verify', arguments: { gateId: 'acceptance', result: 'passed' } } },
      { id: 6, method: 'tools/call', params: { name: 'dev_complete', arguments: {} } },
      { id: 7, method: 'tools/call', params: { name: 'dev_status', arguments: {} } },
    ];
    child.stdin.end(calls.map(c => JSON.stringify({ jsonrpc: '2.0', ...c })).join('\n') + '\n');
    const code = await new Promise<number | null>((done, reject) => { child.once('error', reject); child.once('close', done); });
    assert.equal(code, 0, stderr); const answers = stdout.trim().split('\n').map(s => JSON.parse(s));
    assert.match(answers[2].result.content[0].text, /CONTEXT_REQUIRED/);
    assert.match(answers[4].result.content[0].text, /unknown field/);
    assert.match(answers[5].result.content[0].text, /unknown tool/);
    assert.equal(JSON.parse(answers[6].result.content[0].text).result, 'ready');
    assert.equal(answers[1].result.tools.some((tool: any) => /shell|complete|publish/.test(tool.name)), false);
  }
});

test('a host approval record widened beyond its phase cannot authorize a clean worker scope', t => {
  const { root, store, approval } = fixture(t); (development as any).approveRun(root, store, approval);
  const path = join(store, 'runs', approval.runId, 'approval.json');
  const record = JSON.parse(readFileSync(path, 'utf8')); record.writePaths.push('other/'); writeFileSync(path, JSON.stringify(record));
  assert.throws(() => new development.DevelopmentSession(root, store, approval.runId), /write scope is outside approved DDD phase/);
  const malformed = JSON.parse(readFileSync(path, 'utf8')); malformed.unknown = true; writeFileSync(path, JSON.stringify(malformed));
  assert.throws(() => new development.DevelopmentSession(root, store, approval.runId), /EVIDENCE_INVALID/);
});

test('Git scope verification cannot hide staged or committed edits behind a restored worktree', t => {
  const { root, store, approval, git } = fixture(t); (development as any).approveRun(root, store, approval);
  writeFileSync(join(root, 'owner/input.ts'), 'allowed');
  assert.deepEqual((development as any).verifyWriteScope(root, store, approval.runId).changedPaths, ['owner/input.ts']);
  writeFileSync(join(root, 'other/input.ts'), 'outside'); git('add', 'other/input.ts'); writeFileSync(join(root, 'other/input.ts'), 'outside scope\n');
  assert.throws(() => (development as any).verifyWriteScope(root, store, approval.runId), /WRITE_SCOPE_DENIED/);
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'out of scope');
  assert.throws(() => (development as any).verifyWriteScope(root, store, approval.runId), /WRITE_SCOPE_DENIED/);
});


test('the existing cloud owner can authorize a bounded write without granting repository-root search', async t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.workPackages[0].owners = ['cloud'];
  writeFileSync(join(root, planPath), JSON.stringify(plan));
  git('add', planPath); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'cloud ownership');
  development.approveRun(root, store, { ...approval, owner: 'cloud', baseSha: git('rev-parse', 'HEAD').trim() });
  const session = new development.DevelopmentSession(root, store, approval.runId);
  const current = session.context();
  assert.equal(current.owner, 'cloud');
  const input = session.read('owner/input.ts');
  session.write('owner/input.ts', input.sha256, input.content);
  assert.throws(() => session.search(['.'], 'answer'), /invalid/);
  assert.throws(() => session.read('other/input.ts'), /SCOPE_DENIED/);
});


test('verified producer evidence unblocks its consumer, survives unrelated commits, and invalidates only dependent work', async t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.workPackages[1].startAfter = ['W01'];
  writeFileSync(join(root, planPath), JSON.stringify(plan));
  put(root, 'other/acceptance.test.mjs', "import test from 'node:test';test('consumer behavior',()=>{});\n");
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'handoff baseline');
  const baseSha = git('rev-parse', 'HEAD').trim();
  development.approveRun(root, store, { ...approval, baseSha, coauthorRuns: ['other-run'] });
  development.approveRun(root, store, { ...approval, runId: 'other-run', baseSha,
    selection: { collection: 'workPackages', id: 'W02' }, owner: 'other',
    readPaths: ['other/'], writePaths: ['other/input.ts'],
    gates: [{ id: 'consumer', kind: 'node', inputs: ['other/'], targets: ['other/acceptance.test.mjs'], needs: [] }],
    requires: { W01: { runId: approval.runId } } });
  const producer = new development.DevelopmentSession(root, store, approval.runId);
  const consumer = new development.DevelopmentSession(root, store, 'other-run');
  producer.context(); assert.equal(consumer.context().result, 'blocked');
  await development.dispatchTool(producer, 'dev_verify', { gateId: 'acceptance' });
  assert.equal(consumer.status().result, 'ready');
  await development.dispatchTool(consumer, 'dev_verify', { gateId: 'consumer' });
  assert.equal(consumer.status().result, 'passed');
  const before = readdirSync(join(store, 'runs', approval.runId, 'receipts'));
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'unrelated change');
  producer.context(); consumer.context();
  const reused = await development.dispatchTool(producer, 'dev_verify', { gateId: 'acceptance' }) as any;
  assert.equal(reused.reused, true);
  assert.equal(consumer.status().result, 'passed');
  assert.deepEqual(readdirSync(join(store, 'runs', approval.runId, 'receipts')), before);
  const input = consumer.read('other/input.ts');
  consumer.write('other/input.ts', input.sha256, 'consumer-only change\n');
  assert.equal(producer.status().result, 'passed');
  assert.equal(consumer.status().result, 'ready');
  await development.dispatchTool(consumer, 'dev_verify', { gateId: 'consumer' });
  assert.equal(consumer.status().result, 'passed');
  const source = producer.read('owner/input.ts');
  producer.write('owner/input.ts', source.sha256, 'export const answer = 43;\n');
  assert.equal(producer.status().result, 'ready');
  assert.equal(consumer.status().result, 'blocked');
  await assert.rejects(development.dispatchTool(consumer, 'dev_write', { path: 'other/input.ts', expectedSha256: sha('consumer-only change\n'), content: 'unsafe' }), /DEPENDENCY_BLOCKED/);
});


test('the public dev-scope command invokes scope validation, not an acceptance request', () => {
  const manifest = JSON.parse(readFileSync(new URL('../../package.json', import.meta.url), 'utf8'));
  assert.equal(manifest.scripts['verify:dev-scope'], 'node tools/dev-session.ts scope');
});

async function startVerificationAtBarrier(session: development.DevelopmentSession, marker: string) {
  // Observe only newly materialized runner scratch directories. The unique
  // marker proves the real test has started; elapsed time never releases it.
  const scratch = new Set<string>();
  const watcher = watch(tmpdir(), (_, name) => {
    if (String(name).startsWith('opl-check-scratch-')) scratch.add(join(tmpdir(), String(name), 'tmp'));
  });
  const verification = session.verifyGate('consumer');
  let interval: ReturnType<typeof setInterval> | undefined;
  let deadline: ReturnType<typeof setTimeout> | undefined;
  let scratchPath: string;
  try {
    scratchPath = await Promise.race([
      new Promise<string>((done, reject) => {
        interval = setInterval(() => {
          for (const path of scratch) if (existsSync(join(path, marker))) { done(path); return; }
        }, 10);
        deadline = setTimeout(() => reject(new Error('real acceptance runner did not reach its barrier')), 15_000);
      }),
      verification.then(result => { throw new Error('acceptance completed before its barrier: ' + JSON.stringify(result)); }),
    ]);
  } catch (error) {
    for (const path of scratch) if (existsSync(join(path, marker))) writeFileSync(join(path, marker + '-release'), 'release');
    await verification;
    throw error;
  } finally {
    watcher.close(); clearInterval(interval); clearTimeout(deadline);
  }
  let released = false;
  return { verification, release: () => {
    if (released) return;
    writeFileSync(join(scratchPath, marker + '-release'), 'release'); released = true;
  } };
}

for (const dependency of ['gate.needs', 'cross-run startAfter'] as const) {
  test(`verification never rebinds changed ${dependency} evidence to an already running acceptance`, async t => {
    for (const predecessorState of ['passed', 'pending', 'failed'] as const) {
      await t.test(`predecessor becomes ${predecessorState} during verification`, async t => {
        const { root, store, approval, git } = fixture(t);
        const crossRun = dependency === 'cross-run startAfter';
        const marker = 'consumer-entered-' + randomUUID();
        put(root, 'other/acceptance.test.mjs', `import test from 'node:test';import fs from 'node:fs';import path from 'node:path';
test('consumer acceptance',()=>new Promise(done=>{
  const release=path.join(process.env.TMPDIR,${JSON.stringify(marker + '-release')});
  const interval=setInterval(()=>{if(fs.existsSync(release)){clearInterval(interval);done()}},10);
  fs.writeFileSync(path.join(process.env.TMPDIR,${JSON.stringify(marker)}),'entered');
}));\n`);
        if (crossRun) {
          const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
          plan.workPackages[1].startAfter = ['W01'];
          writeFileSync(join(root, planPath), JSON.stringify(plan));
        }
        git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'race baseline');
        const baseSha = git('rev-parse', 'HEAD').trim();
        const consumerGate = { id: 'consumer', kind: 'node', inputs: ['other/'], targets: ['other/acceptance.test.mjs'], needs: crossRun ? [] : ['acceptance'] };
        development.approveRun(root, store, { ...approval, baseSha, gates: crossRun ? approval.gates : [...approval.gates, consumerGate] });
        const producer = new development.DevelopmentSession(root, store, approval.runId);
        if (crossRun) development.approveRun(root, store, { ...approval, runId: 'other-run', baseSha,
          selection: { collection: 'workPackages', id: 'W02' }, owner: 'other', readPaths: ['other/'], writePaths: ['other/input.ts'],
          gates: [consumerGate], requires: { W01: { runId: approval.runId } } });
        const consumer = crossRun ? new development.DevelopmentSession(root, store, 'other-run') : producer;
        producer.context(); await producer.verifyGate('acceptance'); consumer.context();
        const original = { runId: approval.runId, gateId: 'acceptance', receiptHash: producer.status().stages[0].receiptHash! };
        const consumerInputs = readFileSync(join(root, 'other/input.ts'));
        const barrier = await startVerificationAtBarrier(consumer, marker);
        try {
          const input = producer.read('owner/input.ts');
          producer.write(input.path, input.sha256, predecessorState === 'failed' ? 'export const answer = 43;\n' : input.content + '// changed predecessor inputs\n');
          if (predecessorState !== 'pending') await producer.verifyGate('acceptance');
          assert.equal(producer.status().stages[0].state, predecessorState);
          barrier.release();
          const rejected = await barrier.verification;
          assert.equal(rejected.result, 'failed', JSON.stringify(rejected));
          assert.ok('verification' in rejected);
          assert.equal(rejected.verification.reason, 'DEPENDENCY_CHANGED_DURING_VERIFICATION');
          const receiptDirectory = join(store, 'runs', consumer.approval.runId, 'receipts');
          const receipt = JSON.parse(readFileSync(join(receiptDirectory, 'consumer-1.json'), 'utf8'));
          assert.equal(receipt.result, 'failed');
          assert.deepEqual(receipt.dependencies, [original]);
          const rejectedBytes = readFileSync(join(receiptDirectory, 'consumer-1.json'));
          const restarted = new development.DevelopmentSession(root, store, consumer.approval.runId);
          assert.notEqual(restarted.context().result, 'passed');
          assert.deepEqual(readFileSync(join(root, 'other/input.ts')), consumerInputs);

          if (predecessorState === 'failed') {
            const failed = producer.read('owner/input.ts');
            producer.write(failed.path, failed.sha256, 'export const answer = 42;\n// repaired predecessor inputs\n');
          }
          if (predecessorState !== 'passed') await producer.verifyGate('acceptance');
          const current = { ...original, receiptHash: producer.status().stages[0].receiptHash! };
          assert.notEqual(current.receiptHash, original.receiptHash);
          const retry = await startVerificationAtBarrier(restarted, marker);
          try {
            retry.release();
            assert.equal((await retry.verification).result, 'passed');
          } finally { retry.release(); await retry.verification; }
          const accepted = JSON.parse(readFileSync(join(receiptDirectory, 'consumer-2.json'), 'utf8'));
          assert.equal(accepted.result, 'passed');
          assert.deepEqual(accepted.dependencies, [current]);
          assert.deepEqual(readFileSync(join(receiptDirectory, 'consumer-1.json')), rejectedBytes);
          assert.equal(new development.DevelopmentSession(root, store, consumer.approval.runId).context().result, 'passed');
        } finally { barrier.release(); await barrier.verification; }
      });
    }
  });
}

test('a bounded Console surface fix admits only console paths under its own slice, without business prerequisites', async t => {
  const { root, store, git } = canonicalFixture(t);
  put(root, 'apps/console-ui/src/app/workspace-models-controller-model.ts', 'export const verdictFor = (configuration: string) => configuration;\n');
  put(root, 'tests/ui/workspace-experience-model.test.ts', "import test from 'node:test';import assert from 'node:assert/strict';import { verdictFor } from '../../apps/console-ui/src/app/workspace-models-controller-model.ts';test('owner configuration verdict',()=>assert.equal(verdictFor('applied'),'applied'));\n");
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'console surface slice inputs');
  const baseSha = git('rev-parse', 'HEAD').trim();
  const approval = { schemaVersion: 1, runId: 'console-fix', baseSha, planPath,
    selection: { collection: 'executionSlices', id: 'W16.console-ui-fixes' }, owner: 'console',
    readPaths: ['apps/console-ui/src/app', 'apps/console-ui/src/pages', 'tests/ui'],
    writePaths: ['apps/console-ui/src/app/workspace-models-controller-model.ts'],
    gates: [{ id: 'acceptance', kind: 'node', inputs: ['apps/console-ui/src/app/workspace-models-controller-model.ts', 'tests/ui/workspace-experience-model.test.ts'], targets: ['tests/ui/workspace-experience-model.test.ts'], needs: [] }], requires: {} };
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, 'console-fix');
  const current = session.context();
  assert.equal(current.owner, 'console');
  assert.equal(current.result, 'ready');
  assert.deepEqual(current.blockers, []);
  assert.throws(() => session.read('services/workspace/internal/launch/service.go'), /SCOPE_DENIED/);
  const file = session.read('apps/console-ui/src/app/workspace-models-controller-model.ts');
  session.write(file.path, file.sha256, file.content + '// bounded console presentation fix\n');
  assert.equal((await session.verifyGate('acceptance')).stages[0].state, 'passed');
  assert.throws(() => development.approveRun(root, store, { ...approval, runId: 'console-fix-outside', writePaths: ['services/workspace/internal/launch/service.go'] }), /write scope is outside approved DDD phase/);
  assert.throws(() => development.approveRun(root, store, { ...approval, runId: 'console-fix-borrow', owner: 'workspace', selection: { collection: 'workPackages', id: 'W16' } }), /DDD owner does not own phase record/);
});

test('only the enumerated isolated owner-database fixture may be declared on a Go acceptance gate', async t => {
  const { root, store, git } = canonicalFixture(t);
  // The declaration rides on an existing plan record owned by this shared
  // runner's pilot (W02 owner readiness); no parallel business record is added.
  const base = { schemaVersion: 1, runId: 'owner-db-declaration', planPath,
    selection: { collection: 'executionSlices', id: 'W02.owner-readiness' }, owner: 'serve',
    readPaths: ['services/serve/cmd'], writePaths: ['services/serve/cmd'],
    requires: {} };
  const gate = (extra: Record<string, unknown>) => [{ id: 'owner-suite', kind: 'go', cwd: 'services/serve', inputs: ['services/serve/'], needs: [], ...extra }];
  const baseSha = git('rev-parse', 'HEAD').trim();
  const accepted = { ...base, baseSha, gates: gate({ database: 'isolated-owner-postgres' }) };
  development.approveRun(root, store, accepted);
  const session = new development.DevelopmentSession(root, store, accepted.runId);
  assert.equal(session.approval.gates[0].database, 'isolated-owner-postgres');
  // The declaration is part of the approved record that the host store keeps,
  // so the receipt's approval hash binds which fixture the evidence came from.
  const stored = JSON.parse(readFileSync(join(store, 'runs', accepted.runId, 'approval.json'), 'utf8'));
  assert.equal(stored.gates[0].database, 'isolated-owner-postgres');
  assert.throws(() => development.approveRun(root, store, { ...accepted, runId: 'owner-db-unknown', gates: gate({ database: 'production-postgres' }) }),
    /unknown isolated database fixture/);
  assert.throws(() => development.approveRun(root, store, { ...accepted, runId: 'owner-db-node', gates: [{ id: 'n', kind: 'node', inputs: ['services/serve/'], targets: ['tests/tools/owner-db-declared.test.mjs'], needs: [], database: 'isolated-owner-postgres' }] }),
    /isolated database fixture only applies to Go checks/);
});

test('the development-governance slice resolves to Cloud while its paths stay host-owned for restricted workers', async t => {
  const { root, store, git } = canonicalFixture(t);
  put(root, 'tools/host-entry.ts', 'export const hostOwned = true;\n');
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'governance slice inputs');
  const baseSha = git('rev-parse', 'HEAD').trim();
  const approval = { schemaVersion: 1, runId: 'governance-run', baseSha, planPath,
    selection: { collection: 'executionSlices', id: 'W27.development-governance' }, owner: 'cloud',
    readPaths: ['tools/'], writePaths: ['tools/host-entry.ts'],
    gates: [{ id: 'acceptance', kind: 'developmentPlan', inputs: ['docs/spec/target/checks/development_plan.json'], needs: [] }], requires: {} };
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, 'governance-run');
  assert.equal(session.context().owner, 'cloud');
  const file = session.read('tools/host-entry.ts');
  assert.throws(() => session.write(file.path, file.sha256, 'worker edit'), /SCOPE_DENIED: protected tools\/host-entry\.ts/);
});

test('a malformed dev_context over the real stdio entry revokes the previous admission', async t => {
  const { root, store, approval } = fixture(t); development.approveRun(root, store, approval);
  const child = spawn(process.execPath, [resolve('tools/dev-session.ts'), 'serve', store, approval.runId], { cwd: root, stdio: ['pipe', 'pipe', 'pipe'] });
  t.after(() => child.kill()); let stdout = ''; let stderr = '';
  child.stdout.on('data', b => { stdout += b; }); child.stderr.on('data', b => { stderr += b; });
  const calls = [
    { id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18' } },
    { id: 2, method: 'tools/call', params: { name: 'dev_context', arguments: {} } },
    { id: 3, method: 'tools/call', params: { name: 'dev_read', arguments: { path: 'owner/input.ts' } } },
    { id: 4, method: 'tools/call', params: { name: 'dev_context', arguments: { extra: true } } },
    { id: 5, method: 'tools/call', params: { name: 'dev_read', arguments: { path: 'owner/input.ts' } } },
  ];
  child.stdin.end(calls.map(c => JSON.stringify({ jsonrpc: '2.0', ...c })).join('\n') + '\n');
  const code = await new Promise<number | null>((done, reject) => { child.once('error', reject); child.once('close', done); });
  assert.equal(code, 0, stderr); const answers = stdout.trim().split('\n').map(s => JSON.parse(s));
  assert.equal(answers[2].result.isError, undefined, JSON.stringify(answers[2]));
  assert.match(answers[2].result.content[0].text, /answer/);
  assert.match(answers[3].result.content[0].text, /unknown field/);
  assert.match(answers[4].result.content[0].text, /CONTEXT_REQUIRED/);
});

test('the public scope entry shares baseline, coauthor, prerequisite and write-scope validation with context', t => {
  const { root, store, approval, git } = fixture(t);
  development.approveRun(root, store, approval);
  assert.deepEqual(development.verifyWriteScope(root, store, approval.runId).changedPaths, []);

  const coauthored = { ...approval, runId: 'coauthored-run', coauthorRuns: ['ghost-coauthor'] };
  development.approveRun(root, store, coauthored);
  put(root, 'outside.ts', 'outside both runs\n');
  assert.throws(() => development.verifyWriteScope(root, store, coauthored.runId), /EVIDENCE_INVALID: coauthor or prerequisite run unresolved: ghost-coauthor/);
  assert.throws(() => new development.DevelopmentSession(root, store, coauthored.runId).context(), /EVIDENCE_INVALID: coauthor or prerequisite run unresolved: ghost-coauthor/);
  rmSync(join(root, 'outside.ts'));

  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.workPackages[0].startAfter = ['W02'];
  put(root, planPath, JSON.stringify(plan));
  git('add', planPath); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'declared predecessor');
  const baseSha = git('rev-parse', 'HEAD').trim();
  const required = { ...approval, runId: 'required-run', baseSha, requires: { W02: { runId: 'ghost-predecessor' } } };
  development.approveRun(root, store, required);
  put(root, 'outside.ts', 'outside both runs\n');
  assert.throws(() => development.verifyWriteScope(root, store, required.runId), /EVIDENCE_INVALID: coauthor or prerequisite run unresolved: ghost-predecessor/);
  rmSync(join(root, 'outside.ts'));

  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--amend', '-m', 'rewritten approved history');
  assert.throws(() => development.verifyWriteScope(root, store, required.runId), /BASELINE_MISMATCH/);
});

test('every file and acceptance operation refreshes the current admission before acting', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const operations: ((session: development.DevelopmentSession) => unknown)[] = [
    session => session.read('owner/input.ts'),
    session => session.search(['owner/'], 'answer'),
    session => session.write('owner/new.ts', 'absent', 'out-of-scope-epoch write'),
    session => session.status(),
  ];
  for (const operation of operations) {
    const session = new development.DevelopmentSession(root, store, approval.runId);
    session.context();
    put(root, 'outside.ts', 'not admitted\n');
    assert.throws(() => operation(session), /WRITE_SCOPE_DENIED/);
    rmSync(join(root, 'outside.ts'));
  }
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  put(root, 'outside.ts', 'not admitted\n');
  await assert.rejects(session.verifyGate('acceptance'), /WRITE_SCOPE_DENIED/);
  rmSync(join(root, 'outside.ts'));
});

test('progress never reports passed, and never reuses a gate, without a current host receipt', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  const before = session.context();
  assert.notEqual(before.result, 'passed');
  assert.equal(before.stages[0].state, 'pending');
  await assert.rejects(development.dispatchTool(session, 'dev_complete', {}), /unknown tool/);
  const verified = await session.verifyGate('acceptance') as any;
  assert.equal(verified.result, 'passed');
  assert.match(verified.verification.outputSha256, /^[a-f0-9]{64}$/u);
  rmSync(join(store, 'runs', approval.runId, 'receipts', 'acceptance-1.json'));
  const afterDeletion = new development.DevelopmentSession(root, store, approval.runId).context();
  assert.equal(afterDeletion.result, 'ready');
  assert.equal(afterDeletion.stages[0].state, 'pending');
});

test('the public scope entry re-derives the current phase exactly like context', t => {
  const { root, store, approval, git } = fixture(t);
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  plan.workPackages[0].owners = ['cloud'];
  plan.workPackages[0].plannedWritePaths = ['owner', planPath];
  put(root, planPath, JSON.stringify(plan));
  git('add', planPath); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'plan write granted');
  const granted = { ...approval, baseSha: git('rev-parse', 'HEAD').trim(), owner: 'cloud', writePaths: [...approval.writePaths, planPath] };
  development.approveRun(root, store, granted);
  assert.deepEqual(development.verifyWriteScope(root, store, granted.runId).changedPaths, []);

  const narrowed = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  narrowed.workPackages[0].plannedWritePaths = ['owner/other-only'];
  const session = new development.DevelopmentSession(root, store, granted.runId);
  session.context();
  put(root, planPath, JSON.stringify(narrowed));
  assert.throws(() => session.read('owner/input.ts'), /write scope is outside approved DDD phase: owner\/input\.ts/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
  assert.throws(() => development.verifyWriteScope(root, store, granted.runId), /write scope is outside approved DDD phase: owner\/input\.ts/);
  assert.throws(() => new development.DevelopmentSession(root, store, granted.runId).context(), /write scope is outside approved DDD phase: owner\/input\.ts/);
});

test('host stage receipts bind the run, source, phase, inputs, runner and output, and a foreign run receipt is refused', async t => {
  const { root, store, approval, git } = fixture(t);
  development.approveRun(root, store, approval);
  development.approveRun(root, store, { ...approval, runId: 'second-run' });
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const verified = await session.verifyGate('acceptance');
  assert.equal(verified.result, 'passed');
  const receipt = JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'receipts', 'acceptance-1.json'), 'utf8'));
  assert.equal('payload' in receipt, false, 'host records are plain structured JSON, not signed envelopes');
  assert.equal('signature' in receipt, false, 'no self-signed facility remains');
  const payload = receipt;
  assert.equal(payload.schemaVersion, 1);
  assert.equal(payload.kind, 'opl.development.stage.v1');
  assert.equal(payload.evidenceLayer, 'source');
  assert.equal(payload.runId, approval.runId);
  assert.equal(payload.gateId, 'acceptance');
  assert.equal(payload.attempt, 1);
  assert.equal(payload.result, 'passed');
  assert.equal(payload.sourceSha, git('rev-parse', 'HEAD').trim());
  assert.match(payload.approvalHash, /^[a-f0-9]{64}$/u);
  assert.match(payload.phaseHash, /^[a-f0-9]{64}$/u);
  assert.match(payload.runnerHash, /^[a-f0-9]{64}$/u);
  assert.match(payload.inputHash, /^[a-f0-9]{64}$/u);
  assert.match(payload.verification.outputSha256, /^[a-f0-9]{64}$/u);
  assert.deepEqual(payload.dependencies, []);
  assert.ok(Number.isFinite(Date.parse(payload.checkedAt)));
  const approvalRecord = JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'approval.json'), 'utf8'));
  assert.equal('payload' in approvalRecord, false);
  assert.equal(payload.approvalHash, sha(development.canonical(approvalRecord)));
  assert.equal(payload.sourceSha, git('rev-parse', 'HEAD').trim());

  mkdirSync(join(store, 'runs', 'second-run', 'receipts'), { recursive: true });
  writeFileSync(join(store, 'runs', 'second-run', 'receipts', 'acceptance-1.json'), JSON.stringify(receipt));
  assert.throws(() => new development.DevelopmentSession(root, store, 'second-run').context(), /EVIDENCE_INVALID: invalid stage receipt/);
});

test('the host exports the executed stage result as the pull-request source-check receipt', async t => {
  const { root, store, approval, git } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// executed change\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed');

  // A pending gate never exports a passing receipt.
  development.approveRun(root, store, { ...approval, runId: 'pending-run', gates: [{ ...approval.gates[0], id: 'second' }] });
  assert.throws(() => development.generateSourceCheckReceipt(root, store, 'pending-run', 'second'), /current gate obligation to be passed, not pending/);

  const generated = development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance');
  const written = JSON.parse(readFileSync(join(root, generated.path), 'utf8'));
  assert.equal(generated.path, `docs/evidence/source-checks/${approval.runId}-acceptance-1.json`);
  assert.equal(written.evidenceLayer, 'source');
  assert.equal(written.result, 'pass');
  assert.equal(written.sourceBaseSha, approval.baseSha);
  assert.equal(written.execution.exitCode, 0);
  assert.equal(written.execution.tests, 1);
  assert.equal(written.execution.failed, 0);
  assert.equal(written.execution.skipped, 0);
  assert.equal(written.execution.todo, 0);
  assert.match(written.execution.outputSha256, /^[a-f0-9]{64}$/u);
  // The receipt fingerprints the real changed content and never its own file.
  assert.equal(written.verifiedSource.changedFilesSha256['owner/input.ts'], sha('export const answer = 42;\n// executed change\n'));
  assert.equal('docs/evidence/source-checks/' + approval.runId + '-acceptance-1.json' in written.verifiedSource.changedFilesSha256, false);
  assert.ok(written.writeSet.includes('owner/input.ts'));
  // The generated receipt binds the host record it came from.
  assert.equal(written.sourceStage.runId, approval.runId);
  assert.equal(written.sourceStage.gateId, 'acceptance');
  assert.equal(written.sourceStage.attempt, 1);

  // The same host exit is reachable through the real command entry; the
  // already-exported attempt is refused by the append-only rule.
  assert.throws(
    () => execFileSync(process.execPath, [resolve('tools/dev-session.ts'), 'source-check', store, approval.runId, 'acceptance'], { cwd: root, encoding: 'utf8' }),
    /already exported|append-only/i,
  );

  // A later content change makes the exported receipt stale for the revision.
  writeFileSync(join(root, 'owner/input.ts'), 'export const answer = 42;\n// executed change\n// later change\n');
  const stale = JSON.parse(readFileSync(join(root, generated.path), 'utf8'));
  assert.notEqual(stale.verifiedSource.changedFilesSha256['owner/input.ts'], sha(readFileSync(join(root, 'owner/input.ts'), 'utf8')));
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'receipt export baseline');

});

test('the host records the actually executed command and refuses an export that has none', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// command evidence\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed');

  // The stage receipt carries the command line that actually executed and the
  // executed cwd when it was not the snapshot root; no environment value enters it.
  const receiptPath = join(store, 'runs', approval.runId, 'receipts', 'acceptance-1.json');
  const receipt = JSON.parse(readFileSync(receiptPath, 'utf8'));
  assert.equal(receipt.verification.command, 'node --test --test-reporter=tap owner/acceptance.test.mjs');
  assert.equal(receipt.verification.cwd, undefined);
  assert.equal(JSON.stringify(receipt).includes(root), false);

  // The exported source-check publishes that recorded command verbatim.
  const generated = development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance');
  const written = JSON.parse(readFileSync(join(root, generated.path), 'utf8'));
  assert.equal(written.execution.command, 'node --test --test-reporter=tap owner/acceptance.test.mjs');

  // A historical record written before command capture stays a valid receipt,
  // but it can never be exported as fresh passing evidence.
  development.approveRun(root, store, { ...approval, runId: 'legacy-command' });
  const legacy = new development.DevelopmentSession(root, store, 'legacy-command');
  legacy.context();
  const legacyInput = legacy.read('owner/input.ts');
  legacy.write(legacyInput.path, legacyInput.sha256, legacyInput.content + '// legacy record\n');
  assert.equal((await legacy.verifyGate('acceptance')).result, 'passed');
  const legacyPath = join(store, 'runs', 'legacy-command', 'receipts', 'acceptance-1.json');
  const legacyReceipt = JSON.parse(readFileSync(legacyPath, 'utf8'));
  delete legacyReceipt.verification.command;
  writeFileSync(legacyPath, JSON.stringify(legacyReceipt) + '\n');
  assert.throws(
    () => development.generateSourceCheckReceipt(root, store, 'legacy-command', 'acceptance'),
    /recorded executed command/u,
  );
  assert.equal(existsSync(join(root, 'docs/evidence/source-checks/legacy-command-acceptance-1.json')), false);
});

test('the source-check export re-derives the current obligation and never trusts a stored past result', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// executed change\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed', 'the real gate executed and passed');

  const first = development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance');
  assert.equal(first.path.endsWith('-acceptance-1.json'), true, first.path);
  // Append-only: the same attempt is never regenerated or overwritten.
  const bytes = readFileSync(join(root, first.path));
  assert.throws(() => development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance'), /already exported|append-only/i);
  assert.deepEqual(readFileSync(join(root, first.path)), bytes, 'the recorded attempt stays byte-identical');

  // The stored record still says passed, but the declared input moved on: the
  // export refuses because the current obligation is no longer satisfied.
  const stored = JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'receipts', 'acceptance-1.json'), 'utf8'));
  assert.equal(stored.result, 'passed');
  // Different bytes, same observable behavior: the stored result is now stale
  // for the current declared input even though it still passes the test.
  writeFileSync(join(root, 'owner/input.ts'), 'export const answer = 42;\n// moved on after the receipt\n');
  assert.throws(() => development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance'), /current|stale|pending/i);

  // Re-executing the current obligation produces a new attempt with its own path.
  session.context();
  const current = session.read('owner/input.ts');
  session.write(current.path, current.sha256, current.content + '// re-executed\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed');
  const second = development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance');
  assert.equal(second.path.endsWith('-acceptance-2.json'), true, second.path);
  const secondBody = JSON.parse(readFileSync(join(root, second.path), 'utf8'));
  assert.equal(secondBody.sourceStage.attempt, 2);
  assert.equal(secondBody.verifiedSource.changedFilesSha256['owner/input.ts'], sha(readFileSync(join(root, 'owner/input.ts'), 'utf8')));
});

/**
 * Install one deterministic interleaving point for the export's own Git
 * invocations: while the shim directory is first on PATH, the real Git binary
 * is served by a recorded stand-in that performs one mutation and then
 * delegates. The trigger is either an argument this export path alone passes
 * (`--diff-filter=D`) or the receipt file the append just created, so the
 * mutation can only land between the export's obligation snapshot and its
 * append checks — never before the snapshot and never after the checks.
 */
function gitInterpose(t: any, options: { argv?: string; receipt?: string; rewrite: string; content: string }) {
  const directory = mkdtempSync(join(tmpdir(), 'opl-git-interpose-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const executable = (process.env.PATH ?? '').split(':').filter(Boolean)
    .map(directory_ => join(directory_, 'git'))
    .find(candidate => { try { accessSync(candidate, constants.X_OK); return true; } catch { return false; } });
  if (!executable) throw new Error('git is unavailable on PATH');
  const fired = join(directory, 'fired');
  const trigger = options.argv !== undefined
    ? `process.argv.slice(2).includes(${JSON.stringify(options.argv)})`
    : `existsSync(${JSON.stringify(options.receipt)})`;
  const shim = join(directory, 'git');
  writeFileSync(shim, `#!${process.execPath}
import { spawnSync } from 'node:child_process';
import { existsSync, writeFileSync } from 'node:fs';
if (!existsSync(${JSON.stringify(fired)}) && ${trigger}) {
  writeFileSync(${JSON.stringify(options.rewrite)}, ${JSON.stringify(options.content)});
  writeFileSync(${JSON.stringify(fired)}, 'fired');
}
const delegated = spawnSync(${JSON.stringify(executable)}, process.argv.slice(2), { stdio: ['ignore', 'inherit', 'inherit'] });
process.exit(delegated.status ?? 1);
`);
  chmodSync(shim, 0o755);
  return { directory, fired };
}

test('the source-check export refuses a declared input that moves while the receipt is being built', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// executed change\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed');
  const receiptPath = join(root, 'docs/evidence/source-checks', 'owner-run-acceptance-1.json');

  const moved = 'export const answer = 42;\n// moved while the export enumerated the deletion facts\n';
  const interpose = gitInterpose(t, { argv: '--diff-filter=D', rewrite: join(root, 'owner/input.ts'), content: moved });
  const inherited = process.env.PATH ?? '';
  process.env.PATH = `${interpose.directory}:${inherited}`;
  try {
    assert.throws(() => development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance'), /SOURCE_CHECK_STATE_CHANGED:/u);
  } finally { process.env.PATH = inherited; }

  assert.equal(existsSync(interpose.fired), true, 'the interposed input change ran during the export');
  assert.equal(readFileSync(join(root, 'owner/input.ts'), 'utf8'), moved);
  assert.equal(existsSync(receiptPath), false, 'no record is appended after the obligation moved');
  assert.deepEqual(readdirSync(join(store, 'runs', approval.runId, 'receipts')), ['acceptance-1.json']);
});

test('the source-check export keeps a record whose declared input moves during the append and leaves it stale', async t => {
  const { root, store, git, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// executed change\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed');
  const receiptPath = join(root, 'docs/evidence/source-checks', 'owner-run-acceptance-1.json');
  const receiptRelPath = 'docs/evidence/source-checks/owner-run-acceptance-1.json';

  const moved = 'export const answer = 42;\n// moved after the record was appended\n';
  const interpose = gitInterpose(t, { receipt: receiptPath, rewrite: join(root, 'owner/input.ts'), content: moved });
  const inherited = process.env.PATH ?? '';
  process.env.PATH = `${interpose.directory}:${inherited}`;
  try {
    assert.throws(() => development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance'), /SOURCE_CHECK_STATE_CHANGED_AFTER_APPEND:/u);
  } finally { process.env.PATH = inherited; }

  assert.equal(existsSync(interpose.fired), true, 'the interposed input change ran after the append');
  assert.equal(readFileSync(join(root, 'owner/input.ts'), 'utf8'), moved);
  // Append-only history: the published record is retained untouched, still
  // standing for the inputs that actually executed rather than the moved ones.
  const published = JSON.parse(readFileSync(receiptPath, 'utf8'));
  assert.equal(published.result, 'pass');
  assert.equal(published.sourceStage.attempt, 1);
  assert.equal(published.verifiedSource.changedFilesSha256['owner/input.ts'], sha('export const answer = 42;\n// executed change\n'));
  assert.notEqual(published.verifiedSource.changedFilesSha256['owner/input.ts'], sha(moved));
  // The current acceptance obligation for the affected stage is not passed.
  assert.equal(new development.DevelopmentSession(root, store, approval.runId).context().result, 'ready');
  // The freshness predicate the pull-request record check applies to the very
  // same retained record judges it stale for the checked revision.
  const plan = JSON.parse(readFileSync(join(root, planPath), 'utf8'));
  const sections: Record<string, string> = {
    'Decision Conclusion': 'Record the executed owner acceptance change.',
    'Current Problem': 'The owner behavior needed the admitted change.',
    'Business SSOT': 'No business SSOT change.',
    'Development SSOT': 'No development SSOT change.',
    Baseline: `Base SHA: \`${approval.baseSha}\``,
    Ownership: 'DDD owner: `owner`\nPhase: `W01`',
    'Write Set': `- \`owner/input.ts\`\n- \`${receiptRelPath}\``,
    'Receipt Pipeline': `- receipt: source-check; result: passed; path: \`${receiptRelPath}\``,
    'Acceptance Criteria': '- [x] The owner behavior is verified.',
    Verification: '- `node --test`: executed.',
    Limitations: '- The retained record is stale for the current revision.',
    'Terminal State': 'Terminal state: source-complete',
    'Merge Danger': 'Merge danger: none',
  };
  const body = requiredSections.map(name => `## ${name}\n\n${sections[name]}\n`).join('\n');
  const verdict = checkPullRequestBody(body, {
    plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: approval.baseSha,
    headSha: git('rev-parse', 'HEAD').trim(), mode: 'record',
  });
  assert.equal(verdict.ok, false, 'a staled record is never accepted as current evidence');
  assert.match(verdict.errors.join('\n'), /RECEIPT_STALE_HASH: .*owner\/input\.ts/u);
});

test('the source-check export refuses non-regular changed paths instead of recording a deletion', async t => {
  const { root, store, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// executed change\n');
  assert.equal((await session.verifyGate('acceptance')).result, 'passed');
  symlinkSync(join(root, 'other/input.ts'), join(root, 'owner/new.ts'));
  assert.throws(() => development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance'), /symlink refused|not a regular file/i);
  rmSync(join(root, 'owner/new.ts'));
  const generated = development.generateSourceCheckReceipt(root, store, approval.runId, 'acceptance');
  assert.equal(generated.path.endsWith('-acceptance-1.json'), true);
  assert.equal(JSON.parse(readFileSync(join(root, generated.path), 'utf8')).verifiedSource.changedFilesSha256['owner/new.ts'], undefined);
});

test('a Console surface fix writes only its explicitly granted console test files', async t => {
  const { root, store, git } = canonicalFixture(t);
  put(root, 'apps/console-ui/src/app/workspace-models-controller-model.ts', 'export const verdictFor = (configuration: string) => configuration;\n');
  put(root, 'tests/ui/workspace-experience-model.test.ts', "import test from 'node:test';import assert from 'node:assert/strict';import { verdictFor } from '../../apps/console-ui/src/app/workspace-models-controller-model.ts';test('owner configuration verdict',()=>assert.equal(verdictFor('applied'),'applied'));\n");
  put(root, 'tests/ui/workspace-task-experience-browser.test.ts', "import test from 'node:test';test('browser surface',()=>{});\n");
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'console slice with tests');
  const baseSha = git('rev-parse', 'HEAD').trim();
  const granted = { schemaVersion: 1, runId: 'console-tests', baseSha, planPath,
    selection: { collection: 'executionSlices', id: 'W16.console-ui-fixes' }, owner: 'console',
    readPaths: ['apps/console-ui/src/app', 'tests/ui'],
    writePaths: ['apps/console-ui/src/app/workspace-models-controller-model.ts', 'tests/ui/workspace-experience-model.test.ts'],
    gates: [{ id: 'acceptance', kind: 'node', inputs: ['tests/ui/workspace-experience-model.test.ts', 'apps/console-ui/src/app/workspace-models-controller-model.ts'], targets: ['tests/ui/workspace-experience-model.test.ts'], needs: [] }], requires: {} };
  development.approveRun(root, store, granted);
  const session = new development.DevelopmentSession(root, store, 'console-tests');
  assert.equal(session.context().owner, 'console');
  const implementation = session.read('apps/console-ui/src/app/workspace-models-controller-model.ts');
  session.write(implementation.path, implementation.sha256, implementation.content + '// owner readback derived verdict\n');
  const testFile = session.read('tests/ui/workspace-experience-model.test.ts');
  session.write(testFile.path, testFile.sha256, testFile.content + '// same-owner regression\n');
  assert.equal((await session.verifyGate('acceptance')).stages[0].state, 'passed');

  for (const writePaths of [
    ['tests/ui/workspace-budget-controller-model.test.ts'],
    ['tests/ui/'],
    ['tests/tools/dev-session.test.ts'],
  ]) {
    assert.throws(() => development.approveRun(root, store, { ...granted, runId: `console-denied-${writePaths.length}-${writePaths[0].length}`, writePaths }),
      /write scope is outside approved DDD phase/, writePaths.join(','));
  }
});

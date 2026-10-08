import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import * as development from '../../tools/dev-session.ts';

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
  for (const file of ['AGENTS.md', 'docs/status.md', 'docs/roadmap.md']) put(root, file, `current ${file}\n`);
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
    selection: { collection: 'workPackages', id: 'W01' }, owner: 'owner', readPaths: ['owner/'], writePaths: ['owner/input.ts', 'owner/new.ts'],
    gates: [{ id: 'acceptance', kind: 'node', inputs: ['owner/'], targets: ['owner/acceptance.test.mjs'], needs: [] }], requires: {} };
  return { root, store, git, approval };
}

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

test('owner scope rejects root scans, traversal, symlinks, unknown fields and self-expansion', async t => {
  const { root, store, approval } = fixture(t);
  assert.throws(() => (development as any).approveRun(root, store, { ...approval, runId: 'bad-run', writePaths: ['other/'] }), /outside approved DDD/);
  assert.throws(() => (development as any).approveRun(root, store, { ...approval, runId: 'bad-run', arbitraryCommand: 'true' }), /unknown field/);
  (development as any).approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId); session.context();
  for (const path of ['.', '..', '/', 'owner/../other', '.git/config', '.runtime/key']) assert.throws(() => session.search([path], 'answer'));
  assert.throws(() => session.read('other/input.ts'), /SCOPE_DENIED/);
  symlinkSync(join(root, 'other/input.ts'), join(root, 'owner/link.ts'));
  assert.throws(() => session.read('owner/link.ts'), /symlink refused/);
  assert.throws(() => session.write('owner/acceptance.test.mjs', 'absent', ''), /protected/);
  assert.throws(() => (development as any).approveRun(root, store, approval), /EEXIST/);
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

test('authorization tampering cannot widen a clean worker scope', t => {
  const { root, store, approval } = fixture(t); (development as any).approveRun(root, store, approval);
  const path = join(store, 'runs', approval.runId, 'approval.json');
  const envelope = JSON.parse(readFileSync(path, 'utf8')); envelope.payload.writePaths.push('other/'); writeFileSync(path, JSON.stringify(envelope));
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

import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import { DevelopmentSession, dispatchTool, parseTask, publishReceipt, verifyWriteScope } from '../../tools/dev-session.ts';

const sha = (content: string) => createHash('sha256').update(content).digest('hex');
function fixture(t: any) {
  const root = mkdtempSync(join(tmpdir(), 'opl-dev-session-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'docs')); mkdirSync(join(root, 'owner')); mkdirSync(join(root, 'other')); mkdirSync(join(root, 'development'));
  for (const name of ['AGENTS.md', 'docs/status.md', 'docs/roadmap.md']) writeFileSync(join(root, name), `current ${name}\n`);
  writeFileSync(join(root, 'owner/input.ts'), 'current behavior\n');
  writeFileSync(join(root, 'other/secret.ts'), 'outside scope\n');
  writeFileSync(join(root, 'development/development-contract.json'), JSON.stringify({ schemaVersion: 1, evidenceLayers: ['source'], owners: { owner: { primaryModules: ['owner'], writePaths: ['owner/'], contractPaths: ['docs/status.md'] } } }));
  const task = { schemaVersion: 2, id: 'test-task', objective: 'Fix the first breakpoint', domainOwner: 'owner', primaryModule: 'owner', canonicalContract: 'docs/status.md', callers: ['owner/input.ts'],
    nextAction: 'Read input.ts and change one reachable branch', blockers: ['runtime unverified'],
    context: [{ path: 'AGENTS.md' }, { path: 'docs/status.md' }, { path: 'docs/roadmap.md' }],
    readPaths: ['owner/'], writePaths: ['owner/input.ts', 'owner/new.ts'],
    currentState: { statusPath: 'docs/status.md', roadmapPath: 'docs/roadmap.md', anchors: ['current'] },
    flow: { stages: [{ id: 'read', owner: 'owner', entrypoint: 'read', acceptance: 'read' }, { id: 'done', owner: 'owner', entrypoint: 'test', acceptance: 'test' }], terminalStage: 'done' },
    evidence: { layer: 'source', receiptPath: 'docs/evidence/receipt.json', statusProjection: 'development/state/current.json' }, completionEvidence: 'Focused tests, not production' };
  writeFileSync(join(root, 'task.json'), JSON.stringify(task));
  const git = (...args: string[]) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8' });
  git('init', '-q'); git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'baseline');
  return { root, git, task, session: new DevelopmentSession(root, 'task.json') };
}

test('no read, search or write before mandatory current context delivery', t => {
  const { session } = fixture(t);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
  assert.throws(() => session.search(['owner'], 'current'), /CONTEXT_REQUIRED/);
  assert.throws(() => session.write('owner/input.ts', sha('current behavior\n'), 'new'), /CONTEXT_REQUIRED/);
  const state = session.context();
  assert.equal(state.context.length, 3); assert.match(state.baseSha, /^[a-f0-9]{40}$/);
  assert.equal(state.context[1].content, 'current docs/status.md\n');
  assert.equal(session.read('owner/input.ts').content, 'current behavior\n');
});

test('reject repository-wide search, sibling access, traversal, unknown fields and task modification', t => {
  const { session } = fixture(t); session.context();
  for (const path of ['.', '..', '/', 'owner/../other', '.git/config', 'owner/../../etc/passwd']) {
    assert.throws(() => session.search([path], 'current'));
  }
  assert.throws(() => session.read('other/secret.ts'), /SCOPE_DENIED/);
  assert.throws(() => session.write('other/secret.ts', sha('outside scope\n'), 'bad'), /SCOPE_DENIED/);
  assert.throws(() => session.write('task.json', 'absent', '{}'), /SCOPE_DENIED/);
  assert.throws(() => dispatchTool(session, 'dev_read', { path: 'owner/input.ts', recursive: true }), /unknown field/);
  assert.throws(() => dispatchTool(session, 'shell', { command: 'cat other/secret.ts' }), /unknown tool/);
  assert.equal(session.search(['owner'], 'behavior').hits[0].path, 'owner/input.ts');
});

test('stale source, HEAD or task revokes admission and requires context again', t => {
  const { session, root, git, task } = fixture(t); session.context();
  writeFileSync(join(root, 'docs/status.md'), 'new evidence');
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_STALE/);
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_REQUIRED/);
  session.context();
  writeFileSync(join(root, 'task.json'), JSON.stringify({ ...task, nextAction: 'different' }));
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_STALE/);
  session.context();
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'next');
  assert.throws(() => session.read('owner/input.ts'), /CONTEXT_STALE/);
});

test('writes use compare-and-set and reject symlinks, including parent directory links', t => {
  const { session, root } = fixture(t); session.context();
  const before = session.read('owner/input.ts');
  writeFileSync(join(root, 'owner/input.ts'), 'concurrent change');
  assert.throws(() => session.write('owner/input.ts', before.sha256, 'lost update'), /WRITE_CONFLICT/);
  const current = session.read('owner/input.ts');
  session.write('owner/input.ts', current.sha256, 'correct change');
  assert.equal(readFileSync(join(root, 'owner/input.ts'), 'utf8'), 'correct change');
  symlinkSync(join(root, 'other/secret.ts'), join(root, 'owner/new.ts'));
  assert.throws(() => session.write('owner/new.ts', 'absent', 'escape'), /symlink refused/);
  symlinkSync(join(root, 'other'), join(root, 'owner/link'));
  assert.throws(() => session.read('owner/link/secret.ts'), /symlink refused/);
});

test('search fails explicitly instead of silently truncating over-budget results', t => {
  const { session, root } = fixture(t); session.context();
  for (let index = 0; index < 65; index++) writeFileSync(join(root, `owner/${index}.txt`), 'match');
  assert.throws(() => session.search(['owner'], 'match'), /SEARCH_BUDGET/);
});

test('task validation requires current owners, exact version and bounded paths', t => {
  const { task } = fixture(t);
  assert.throws(() => parseTask({ ...task, context: [] }), /missing current context/);
  assert.throws(() => parseTask({ ...task, readPaths: ['.'] }), /invalid/);
  assert.throws(() => parseTask({ ...task, writePaths: ['**'] }), /invalid/);
  assert.throws(() => parseTask({ ...task, ignoredFlag: true }), /unknown field/);
  for (const id of [undefined, null, true]) assert.throws(() => parseTask({ ...task, id }), /invalid task/);
  assert.throws(() => parseTask({ ...task, context: [{ path: 'AGENTS.md', startLine: 1, endLine: 2 }] }), /full current input/);
});

test('scope gate detects staged, unstaged, committed and untracked changes without trusting edited task', t => {
  const { root, git, task } = fixture(t); const base = git('rev-parse', 'HEAD').trim();
  writeFileSync(join(root, 'owner/input.ts'), 'valid change');
  assert.deepEqual(verifyWriteScope(root, 'task.json', base).changedPaths, ['owner/input.ts']);
  writeFileSync(join(root, 'other/new.ts'), 'outside scope');
  assert.throws(() => verifyWriteScope(root, 'task.json', base), /WRITE_SCOPE_DENIED/);
  rmSync(join(root, 'other/new.ts'));
  writeFileSync(join(root, 'other/secret.ts'), 'bad'); git('add', 'other');
  assert.throws(() => verifyWriteScope(root, 'task.json', base), /WRITE_SCOPE_DENIED/);
  git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'outside');
  assert.throws(() => verifyWriteScope(root, 'task.json', base), /WRITE_SCOPE_DENIED/);
  writeFileSync(join(root, 'task.json'), JSON.stringify({ ...task, writePaths: ['other/'] }));
  assert.throws(() => verifyWriteScope(root, 'task.json', base), /WRITE_SCOPE_DENIED/);
});

test('scope gate reads base permissions rather than a clean self-expanded task', t => {
  const { root, git, task } = fixture(t); const base = git('rev-parse', 'HEAD').trim();
  writeFileSync(join(root, 'task.json'), JSON.stringify({ ...task, writePaths: ['other/'] }));
  writeFileSync(join(root, 'other/secret.ts'), 'new unauthorized bytes');
  assert.throws(() => verifyWriteScope(root, 'task.json', base), /WRITE_SCOPE_DENIED: other\/secret.ts/);
  assert.throws(() => verifyWriteScope(root, 'task.json', 'HEAD'), /exact approved base SHA/);
});

test('scope gate cannot hide an out-of-scope staged edit behind a restored worktree', t => {
  const { root, git } = fixture(t); const base = git('rev-parse', 'HEAD').trim();
  writeFileSync(join(root, 'other/secret.ts'), 'staged out of scope'); git('add', 'other/secret.ts');
  writeFileSync(join(root, 'other/secret.ts'), 'outside scope\n');
  assert.throws(() => verifyWriteScope(root, 'task.json', base), /WRITE_SCOPE_DENIED/);
});

test('returned context cannot mutate the admitted authorization snapshot', t => {
  const { session } = fixture(t);
  const state = session.context(); state.readPaths.push('other/'); state.writePaths.push('other/');
  assert.throws(() => session.read('other/secret.ts'), /SCOPE_DENIED/);
});

test('task placed inside an authorized directory remains protected, including case aliases', t => {
  const { root, task } = fixture(t);
  task.writePaths = ['owner/'];
  writeFileSync(join(root, 'owner/task.json'), JSON.stringify(task));
  const session = new DevelopmentSession(root, 'owner/task.json'); session.context();
  assert.throws(() => session.write('owner/task.json', sha(JSON.stringify(task)), '{}'), /SCOPE_DENIED/);
  if (existsSync(join(root, 'owner/TASK.JSON'))) {
    assert.throws(() => session.write('owner/TASK.JSON', sha(JSON.stringify(task)), '{}'), /SCOPE_DENIED/);
  } else {
    session.write('owner/TASK.JSON', 'absent', '{}');
    assert.equal(readFileSync(join(root, 'owner/task.json'), 'utf8'), JSON.stringify(task));
  }
});

test('trusted completion receipt updates the machine state and generated status projection', t => {
  const { root, session, task } = fixture(t);
  const context = session.context();
  const result = publishReceipt(root, 'task.json', {
    schemaVersion: 1,
    taskId: task.id,
    sourceSha: context.baseSha,
    result: 'passed',
    evidenceLayer: 'source',
    checkedAt: '2026-10-08T00:00:00+08:00',
    commands: [{ command: 'node --test', result: 'passed' }],
    changedPaths: ['owner/input.ts'],
    summary: 'Focused owner tests passed.'
  });
  assert.equal(result.result, 'passed');
  assert.match(readFileSync(join(root, 'development/state/current.json'), 'utf8'), /test-task/);
  assert.match(readFileSync(join(root, 'docs/status.md'), 'utf8'), /GENERATED DEVELOPMENT STATE/);
  assert.match(readFileSync(join(root, 'docs/status.md'), 'utf8'), /evidence\/receipt.json/);
  assert.throws(() => session.write('docs/status.md', sha('current docs/status.md\n'), 'agent edit'), /CONTEXT_STALE/);
  session.context();
  assert.throws(() => session.write('docs/status.md', sha('current docs/status.md\n'), 'agent edit'), /SCOPE_DENIED/);
});

test('real stdio runner denies tools before context and exposes no shell', async t => {
  const { root } = fixture(t);
  const child = spawn(process.execPath, [resolve('tools/dev-session.ts'), 'serve', 'task.json'], { cwd: root, stdio: ['pipe', 'pipe', 'pipe'] });
  t.after(() => child.kill());
  const messages = [
    { id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05' } },
    { id: 2, method: 'tools/list' },
    { id: 3, method: 'tools/call', params: { name: 'dev_read', arguments: { path: 'owner/input.ts' } } },
    { id: 4, method: 'tools/call', params: { name: 'dev_context', arguments: {} } },
    { id: 5, method: 'tools/call', params: { name: 'dev_read', arguments: { path: 'owner/input.ts' } } },
    { id: 6, method: 'tools/call', params: { name: 'dev_search', arguments: { paths: ['.'], literal: 'current' } } },
  ];
  let output = ''; let stderr = '';
  child.stdout.on('data', data => { output += data; }); child.stderr.on('data', data => { stderr += data; });
  child.stdin.end(messages.map(message => JSON.stringify({ jsonrpc: '2.0', ...message })).join('\n') + '\n');
  const code = await new Promise<number | null>((done, reject) => { child.on('error', reject); child.on('close', done); });
  assert.equal(code, 0, stderr);
  const responses = output.trim().split('\n').map(line => JSON.parse(line));
  assert.equal(responses.length, 6);
  assert.deepEqual(responses[1].result.tools.map((tool: any) => tool.name), ['dev_context', 'dev_read', 'dev_search', 'dev_write']);
  assert.equal(responses[2].result.isError, true); assert.match(responses[2].result.content[0].text, /CONTEXT_REQUIRED/);
  assert.equal(responses[4].result.isError, undefined);
  assert.equal(responses[5].result.isError, true);
});

test('source CI typechecks the development tools and the freshness gate stays callable', () => {
  const verification = readFileSync('tools/verify-local.ts', 'utf8');
  assert.match(verification, /name: "Development tools typecheck", command: "npm", args: \["run", "typecheck:development-tools"\]/);
  // The generated-contract gate is callable and version-pinned, but is not wired as a
  // blocking required check yet: the production proto and the target specification still
  // carry a real, owner-reconciled semantic drift (see the contracts owner). Wiring it as a
  // hard CI gate would red the shared main branch before that reconciliation lands.
  const pkg = JSON.parse(readFileSync('package.json', 'utf8'));
  assert.equal(pkg.scripts['verify:generated-contracts'], 'node tools/verify-generated-contracts.ts');
  const workflow = readFileSync('.github/workflows/pull-request-ci.yml', 'utf8');
  assert.doesNotMatch(workflow, /npm run verify:generated-contracts/);
});

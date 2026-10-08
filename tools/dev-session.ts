/** Development admission and evidence are host-owned; business plans remain their existing owners. */
import { createHash, createPrivateKey, createPublicKey, generateKeyPairSync, randomUUID, sign, verify } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { closeSync, constants, existsSync, fstatSync, ftruncateSync, lstatSync, linkSync, mkdirSync, mkdtempSync, openSync, readFileSync, readdirSync, realpathSync, renameSync, rmSync, unlinkSync, writeFileSync, writeSync } from 'node:fs';
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path';
import { tmpdir } from 'node:os';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';

const digest = (value: string | Buffer) => createHash('sha256').update(value).digest('hex');
function fail(message: string): never { throw new Error(message); }
const git = (root: string, args: string[]) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
const object = (value: unknown): Record<string, any> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail('expected object');
  return value as Record<string, any>;
};
function keys(value: Record<string, any>, allowed: string[]) {
  for (const key of Object.keys(value)) if (!allowed.includes(key)) fail(`unknown field: ${key}`);
}
function strings(value: unknown, label: string, nonempty = true): string[] {
  if (!Array.isArray(value) || (nonempty && !value.length) || value.some(item => typeof item !== 'string' || !item)) fail(`invalid ${label}`);
  if (new Set(value).size !== value.length) fail(`duplicate ${label}`);
  return value;
}
function id(value: unknown): string {
  if (typeof value !== 'string' || !/^[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)*$/u.test(value)) fail('invalid identifier');
  return value;
}
function pathName(value: unknown, directory = false): string {
  if (typeof value !== 'string' || !value || isAbsolute(value) || value.includes('\\') || /[\x00-\x1f*?\[\]{}]/u.test(value)) fail('invalid relative path');
  const name = directory && value.endsWith('/') ? value.slice(0, -1) : value;
  if (name.split('/').some(p => !p || p === '.' || p === '..') || ['.git', '.runtime', 'node_modules'].some(p => name === p || name.startsWith(p + '/')) || name.split('/').some(p => p === '.env' || p.startsWith('.env.'))) fail('invalid or protected path');
  return value;
}
const matches = (path: string, roots: string[]) => roots.some(root => root.endsWith('/') ? path.startsWith(root) || path === root.slice(0, -1) : path === root);
function canonical(value: any): string {
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.keys(value).sort().map(k => JSON.stringify(k) + ':' + canonical(value[k])).join(',') + '}';
  return JSON.stringify(value);
}
export interface DevelopmentGate {
  id: string; kind: 'node' | 'go' | 'browser' | 'generated' | 'developmentPlan'; inputs: string[]; targets?: string[]; cwd?: string; needs: string[];
}
/** A host execution authorization references a phase record; it never copies business ownership or progress. */
export interface RunApproval {
  schemaVersion: 1; runId: string; baseSha: string; planPath: string;
  selection: { collection: 'workPackages' | 'executionSlices' | 'parallelPreparation'; id: string };
  owner: string; readPaths: string[]; writePaths: string[]; gates: DevelopmentGate[];
  requires: Record<string, { runId: string; gateId?: string }>;
}
function parseApproval(value: unknown): RunApproval {
  const a = object(value); keys(a, ['schemaVersion', 'runId', 'baseSha', 'planPath', 'selection', 'owner', 'readPaths', 'writePaths', 'gates', 'requires']);
  if (a.schemaVersion !== 1 || !/^[a-f0-9]{40}$/u.test(a.baseSha)) fail('exact approved base SHA required');
  id(a.runId); id(a.owner); pathName(a.planPath);
  const s = object(a.selection); keys(s, ['collection', 'id']); id(s.id);
  if (!['workPackages', 'executionSlices', 'parallelPreparation'].includes(s.collection)) fail('invalid phase collection');
  for (const field of ['readPaths', 'writePaths']) strings(a[field], field).forEach(p => pathName(p, true));
  if (!Array.isArray(a.gates) || !a.gates.length) fail('acceptance gates required');
  const seen = new Set<string>();
  for (const raw of a.gates) {
    const g = object(raw); keys(g, ['id', 'kind', 'inputs', 'targets', 'cwd', 'needs']); id(g.id);
    if (seen.has(g.id)) fail('duplicate gate'); seen.add(g.id);
    if (!['node', 'go', 'browser', 'generated', 'developmentPlan'].includes(g.kind)) fail('unknown acceptance runner');
    strings(g.inputs, 'gate inputs').forEach(p => pathName(p, true)); strings(g.needs, 'gate dependencies', false).forEach(id);
    if (g.kind === 'node') strings(g.targets, 'test targets').forEach(p => pathName(p));
    else if (g.targets !== undefined) fail('targets only apply to node tests');
    if (g.kind === 'go') pathName(g.cwd); else if (g.cwd !== undefined) fail('cwd only applies to Go checks');
  }
  const pending = new Map<string, Set<string>>(a.gates.map((g: DevelopmentGate) => [g.id, new Set(g.needs)]));
  while (pending.size) {
    const ready = [...pending].filter(([, deps]) => !deps.size).map(([key]) => key);
    if (!ready.length) fail('unknown or cyclic gate dependency');
    ready.forEach(key => pending.delete(key)); pending.forEach(deps => ready.forEach(key => deps.delete(key)));
  }
  for (const [dependency, raw] of Object.entries(object(a.requires))) {
    id(dependency); const r = object(raw); keys(r, ['runId', 'gateId']); id(r.runId); if (r.gateId !== undefined) id(r.gateId);
    if (r.runId === a.runId) fail('self dependency refused');
  }
  return a as RunApproval;
}
interface Phase { record: Record<string, any>; root: string; startAfter: string[]; acceptAfter: string[]; fingerprint: string }
function phase(root: string, approval: RunApproval, atBase = false): Phase {
  const bytes = atBase ? git(root, ['show', `${approval.baseSha}:${approval.planPath}`]) : fileBytes(root, approval.planPath, 2 * 1024 * 1024).toString();
  const plan = object(JSON.parse(bytes));
  if (plan.schemaVersion !== 1) fail('unsupported phase plan');
  const roots = object(plan.sourceRoots); const ownerRoot = pathName(roots[approval.owner]);
  const list = plan[approval.selection.collection]; if (!Array.isArray(list)) fail('phase collection missing');
  const found = list.filter((item: any) => (item.id ?? item.window) === approval.selection.id);
  if (found.length !== 1) fail('phase record missing or duplicated');
  const record = object(found[0]);
  const parent = approval.selection.collection === 'executionSlices' ? (plan.workPackages || []).find((w: any) => w.id === record.workPackage) : record;
  if (approval.selection.collection !== 'parallelPreparation' && (!parent || !Array.isArray(parent.owners) || !parent.owners.includes(approval.owner))) fail('DDD owner does not own phase record');
  const writes = strings(record.plannedWritePaths ?? record.writePaths, 'phase write scope');
  const normalized = writes.map(p => {
    pathName(p, true);
    // A nonexistent declaration remains an exact file, never guessed to be a directory grant.
    const target = physical(root, p.replace(/\/$/u, ''));
    return p.endsWith('/') || (existsSync(target) && lstatSync(target).isDirectory()) ? p.replace(/\/$/u, '') + '/' : p;
  });
  for (const p of approval.writePaths) {
    if (!matches(p.replace(/\/$/u, ''), [ownerRoot + '/']) || !matches(p.replace(/\/$/u, ''), normalized)) fail(`write scope is outside approved DDD phase: ${p}`);
    const forbidden = record.forbiddenWrites ?? [];
    if (forbidden.some((q: string) => p === q || p.startsWith(q + '/') || q.startsWith(p.endsWith('/') ? p : p + '/'))) fail(`phase forbids write: ${p}`);
  }
  const startAfter = strings(record.startAfter ?? [], 'start dependencies', false);
  const acceptAfter = strings(record.acceptAfter ?? (record.sharedContractGate ? [record.sharedContractGate] : []), 'accept dependencies', false);
  const dependencies = new Set([...startAfter, ...acceptAfter]);
  for (const key of Object.keys(approval.requires)) if (!dependencies.has(key)) fail(`invented phase dependency: ${key}`);
  return { record, root: ownerRoot, startAfter, acceptAfter, fingerprint: digest(canonical({ record, ownerRoot })) };
}
function physical(root: string, path: string) {
  pathName(path); let target = root;
  for (const part of path.split('/')) {
    target = resolve(target, part);
    if (existsSync(target) && lstatSync(target).isSymbolicLink()) fail(`symlink refused: ${path}`);
    // existsSync follows dangling links; lstat must also check that case.
    if (!existsSync(target)) { try { if (lstatSync(target).isSymbolicLink()) fail(`symlink refused: ${path}`); } catch (e: any) { if (e.code !== 'ENOENT') throw e; } }
  }
  const rel = relative(root, target); if (!rel || rel === '..' || rel.startsWith('..' + sep) || isAbsolute(rel)) fail('path escapes repository');
  return target;
}
function fileBytes(root: string, path: string, limit = 192 * 1024): Buffer {
  const fd = openSync(physical(root, path), constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = fstatSync(fd); if (!stat.isFile() || stat.nlink !== 1 || stat.size > limit) fail(`invalid regular file or budget: ${path}`);
    return readFileSync(fd);
  } finally { closeSync(fd); }
}
function storeRoot(root: string, store: string) {
  if (!isAbsolute(store)) fail('absolute host store required');
  const target = resolve(store); mkdirSync(target, { recursive: true, mode: 0o700 });
  const real = realpathSync(target); const repo = realpathSync(root);
  if (real === repo || real.startsWith(repo + sep) || repo.startsWith(real + sep)) fail('host store must be separate from repository');
  return real;
}
function signed(store: string, payload: unknown) {
  const key = createPrivateKey(readFileSync(resolve(store, 'authority.key')));
  return { payload, signature: sign(null, Buffer.from(canonical(payload)), key).toString('base64') };
}
function readSigned(store: string, path: string): any {
  const envelope = object(JSON.parse(readFileSync(path, 'utf8'))); keys(envelope, ['payload', 'signature']);
  const key = createPublicKey(readFileSync(resolve(store, 'authority.pub')));
  if (typeof envelope.signature !== 'string' || !verify(null, Buffer.from(canonical(envelope.payload)), key, Buffer.from(envelope.signature, 'base64'))) fail('EVIDENCE_INVALID: host signature mismatch');
  return envelope.payload;
}
function runDirectory(store: string, runId: string) { return resolve(store, 'runs', id(runId)); }
/** Approve a bounded execution reference before launching the restricted worker. Only the host exposes this operation. */
export function approveRun(root: string, storePath: string, raw: unknown) {
  root = realpathSync(root); const approval = parseApproval(raw); phase(root, approval, true);
  const store = storeRoot(root, storePath);
  if (!existsSync(resolve(store, 'authority.key'))) {
    const pair = generateKeyPairSync('ed25519');
    writeFileSync(resolve(store, 'authority.key'), pair.privateKey.export({ type: 'pkcs8', format: 'pem' }), { flag: 'wx', mode: 0o600 });
    writeFileSync(resolve(store, 'authority.pub'), pair.publicKey.export({ type: 'spki', format: 'pem' }), { flag: 'wx', mode: 0o600 });
  }
  const directory = runDirectory(store, approval.runId); mkdirSync(directory, { recursive: true, mode: 0o700 });
  writeFileSync(resolve(directory, 'approval.json'), JSON.stringify(signed(store, approval)) + '\n', { flag: 'wx', mode: 0o600 });
  return { runId: approval.runId, hostStore: store };
}
interface StageReceipt {
  schemaVersion: 1; kind: 'opl.development.stage.v1'; runId: string; gateId: string; attempt: number;
  evidenceLayer: 'source'; sourceSha: string; approvalHash: string; phaseHash: string; runnerHash: string;
  inputHash: string; dependencies: { runId: string; gateId: string; receiptHash: string }[];
  result: 'passed' | 'failed' | 'blocked'; checkedAt: string;
  verification: { tests: number; failed: number; skipped: number; exitCode: number | null; outputSha256: string; reason?: string };
}
function inputFiles(root: string, paths: string[]) {
  const files = new Map<string, Buffer>(); let bytes = 0;
  const walk = (path: string) => {
    const target = physical(root, path); const stat = lstatSync(target);
    if (stat.isDirectory()) {
      for (const name of readdirSync(target).sort()) {
        if (['.git', '.runtime', 'node_modules', '.DS_Store', '__pycache__'].includes(name)) continue;
        walk(`${path}/${name}`);
      }
    } else {
      const content = fileBytes(root, path, 16 * 1024 * 1024); bytes += content.length;
      if (bytes > 128 * 1024 * 1024 || files.size >= 4096) fail('declared stage input budget exceeded');
      files.set(path, content);
    }
  };
  for (const path of paths) { pathName(path, true); walk(path.replace(/\/$/u, '')); }
  return files;
}
function fingerprint(files: Map<string, Buffer>) {
  return digest(canonical([...files].sort(([a], [b]) => a.localeCompare(b)).map(([path, bytes]) => ({ path, sha256: digest(bytes) }))));
}
function runnerHash() {
  return digest(readFileSync(fileURLToPath(import.meta.url)) + '\n' + readFileSync(fileURLToPath(new URL('./verify-local.ts', import.meta.url))));
}
function receiptHistory(store: string, runId: string, gateId: string): { receipt: StageReceipt; hash: string }[] {
  const directory = resolve(runDirectory(store, runId), 'receipts'); if (!existsSync(directory)) return [];
  const results = readdirSync(directory).filter(path => path.endsWith('.json')).map(path => {
    const receipt = readSigned(store, resolve(directory, path)) as StageReceipt;
    if (receipt.schemaVersion !== 1 || receipt.kind !== 'opl.development.stage.v1' || receipt.runId !== runId ||
      !Number.isSafeInteger(receipt.attempt) || receipt.attempt < 1 || !['passed', 'failed', 'blocked'].includes(receipt.result) || receipt.evidenceLayer !== 'source') fail('EVIDENCE_INVALID: invalid stage receipt');
    return { receipt, hash: digest(canonical(receipt)) };
  }).filter(item => item.receipt.gateId === gateId).sort((a, b) => a.receipt.attempt - b.receipt.attempt);
  if (results.some((item, index) => item.receipt.attempt !== index + 1)) fail('EVIDENCE_INVALID: receipt sequence gap or duplicate');
  return results;
}
function appendReceipt(store: string, receipt: StageReceipt) {
  const directory = resolve(runDirectory(store, receipt.runId), 'receipts'); mkdirSync(directory, { recursive: true, mode: 0o700 });
  const target = resolve(directory, `${receipt.gateId}-${receipt.attempt}.json`);
  const temporary = resolve(directory, `.pending-${randomUUID()}`);
  writeFileSync(temporary, JSON.stringify(signed(store, receipt)) + '\n', { flag: 'wx', mode: 0o600 });
  try { linkSync(temporary, target); } finally { unlinkSync(temporary); }
}

/** Each new worker starts unadmitted, including after restart. There is no agent-controlled completion method. */
export class DevelopmentSession {
  readonly root: string; readonly store: string; readonly approval: RunApproval;
  private admitted?: { head: string; phase: string; inputs: { path: string; sha256: string }[] };
  constructor(root: string, store: string, runId: string) {
    this.root = realpathSync(root); this.store = storeRoot(this.root, store);
    this.approval = parseApproval(readSigned(this.store, resolve(runDirectory(this.store, runId), 'approval.json')));
    if (this.approval.runId !== runId) fail('run identity mismatch'); phase(this.root, this.approval, true);
  }
  private head() { return git(this.root, ['rev-parse', 'HEAD']).trim(); }
  context() {
    const selected = phase(this.root, this.approval); const head = this.head();
    const inputs = ['AGENTS.md', 'docs/status.md', 'docs/roadmap.md'].map(path => {
      const bytes = fileBytes(this.root, path); return { path, sha256: digest(bytes), content: bytes.toString() };
    });
    if (Buffer.byteLength(JSON.stringify(inputs)) > 192 * 1024) fail('required context exceeds budget');
    if (head !== this.head() || inputs.some(i => digest(fileBytes(this.root, i.path)) !== i.sha256)) fail('CONTEXT_STALE');
    this.admitted = { head, phase: selected.fingerprint, inputs: inputs.map(({ path, sha256 }) => ({ path, sha256 })) };
    return { runId: this.approval.runId, owner: this.approval.owner, selection: this.approval.selection,
      baseSha: this.approval.baseSha, observedHead: head, context: inputs, phaseRecord: selected.record,
      readPaths: this.approval.readPaths, writePaths: this.approval.writePaths, evidenceLayer: 'source',
      ...this.view() };
  }
  private ready() {
    if (!this.admitted) fail('CONTEXT_REQUIRED: receive current context first');
    if (this.head() !== this.admitted.head || phase(this.root, this.approval).fingerprint !== this.admitted.phase ||
      this.admitted.inputs.some(i => digest(fileBytes(this.root, i.path)) !== i.sha256)) {
      this.admitted = undefined; fail('CONTEXT_STALE: receive current context again');
    }
  }
  private gateState(gateId: string, visiting = new Set<string>()): { gateId: string; state: string; receiptHash?: string; blockers: any[] } {
    const marker = `${this.approval.runId}:${gateId}`; if (visiting.has(marker)) fail('EVIDENCE_INVALID: cyclic evidence dependencies');
    visiting = new Set(visiting).add(marker);
    const gate = this.approval.gates.find(g => g.id === gateId); if (!gate) fail('unknown gate');
    const external = this.dependencyEvidence(phase(this.root, this.approval).startAfter, visiting);
    const blockers: any[] = [...external.blockers]; const dependencies: StageReceipt['dependencies'] = [...external.dependencies];
    for (const dependency of gate.needs) {
      const prior = this.gateState(dependency, visiting);
      if (prior.state !== 'passed') blockers.push({ dependency, owner: this.approval.owner, state: prior.state });
      else dependencies.push({ runId: this.approval.runId, gateId: dependency, receiptHash: prior.receiptHash! });
    }
    if (blockers.length) return { gateId, state: 'blocked', blockers };
    const last = receiptHistory(this.store, this.approval.runId, gateId).at(-1);
    if (!last) return { gateId, state: 'pending', blockers };
    const selected = phase(this.root, this.approval);
    const r = last.receipt;
    if (r.approvalHash !== digest(canonical(this.approval)) || r.phaseHash !== selected.fingerprint || r.runnerHash !== runnerHash() ||
      r.inputHash !== fingerprint(inputFiles(this.root, gate.inputs)) || canonical(r.dependencies) !== canonical(dependencies)) return { gateId, state: 'pending', blockers };
    if (r.result === 'blocked') blockers.push({ dependency: gateId, owner: this.approval.owner, reason: r.verification.reason ?? 'acceptance runner blocked' });
    return { gateId, state: r.result, receiptHash: last.hash, blockers };
  }
  private dependencyEvidence(ids: string[], visiting = new Set<string>()) {
    const blockers: any[] = []; const dependencies: StageReceipt['dependencies'] = [];
    for (const dependency of ids) {
      const reference = this.approval.requires[dependency];
      if (!reference || !existsSync(resolve(runDirectory(this.store, reference.runId), 'approval.json'))) {
        blockers.push({ dependency, owner: 'unresolved', reason: 'approved predecessor receipt required' }); continue;
      }
      const upstream = new DevelopmentSession(this.root, this.store, reference.runId);
      if (upstream.approval.selection.id !== dependency) fail('EVIDENCE_INVALID: predecessor phase identity mismatch');
      const states = reference.gateId ? [upstream.gateState(reference.gateId, visiting)] : upstream.approval.gates.map(g => upstream.gateState(g.id, visiting));
      if (states.some(s => s.state !== 'passed')) {
        blockers.push({ dependency, owner: upstream.approval.owner, runId: reference.runId, reason: 'predecessor evidence missing, stale, failed or blocked' }); continue;
      }
      if (!reference.gateId) {
        const terminal = upstream.dependencyEvidence(phase(this.root, upstream.approval).acceptAfter, visiting);
        if (terminal.blockers.length) { blockers.push({ dependency, owner: upstream.approval.owner, reason: 'predecessor terminal dependencies unverified' }); continue; }
      }
      dependencies.push(...states.map(state => ({ runId: reference.runId, gateId: state.gateId, receiptHash: state.receiptHash! })));
    }
    return { blockers, dependencies };
  }
  private view() {
    const stages = this.approval.gates.map(g => this.gateState(g.id));
    const terminal = this.dependencyEvidence(phase(this.root, this.approval).acceptAfter);
    const passed = stages.every(s => s.state === 'passed') && !terminal.blockers.length;
    const next = stages.find(s => s.state !== 'passed' && s.state !== 'blocked');
    return { result: passed ? 'passed' : next?.state === 'failed' ? 'failed' : next ? 'ready' : 'blocked', nextGate: next?.gateId ?? null,
      stages, blockers: [...stages.flatMap(s => s.blockers), ...terminal.blockers] };
  }
  status() { this.ready(); return this.view(); }
  /** A request executes the approved runner; caller-supplied results are never accepted. */
  async verifyGate(gateId: string) {
    this.ready(); const gate = this.approval.gates.find(g => g.id === gateId); if (!gate) fail('unknown gate');
    const state = this.gateState(gateId);
    if (state.state === 'blocked') fail('DEPENDENCY_BLOCKED: ' + JSON.stringify(state.blockers));
    if (state.state === 'passed') return { ...this.view(), reused: true };
    const directory = runDirectory(this.store, this.approval.runId);
    const lock = resolve(directory, `${id(gateId)}.lock`);
    const fd = openSync(lock, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL, 0o600);
    writeFileSync(fd, JSON.stringify({ pid: process.pid })); closeSync(fd);
    let snapshot: string | undefined;
    try {
      const files = inputFiles(this.root, gate.inputs); const hash = fingerprint(files);
      const phaseHash = phase(this.root, this.approval).fingerprint;
      snapshot = mkdtempSync(resolve(tmpdir(), 'opl-acceptance-'));
      for (const [path, bytes] of files) {
        const target = resolve(snapshot, path); mkdirSync(dirname(target), { recursive: true }); writeFileSync(target, bytes, { mode: 0o444 });
      }
      const { runDevelopmentCheck } = await import('./verify-local.ts');
      const checked = await runDevelopmentCheck({ snapshotRoot: snapshot, kind: gate.kind, targets: gate.targets, cwd: gate.cwd });
      const unchanged = hash === fingerprint(inputFiles(this.root, gate.inputs)) && phaseHash === phase(this.root, this.approval).fingerprint;
      const previous = receiptHistory(this.store, this.approval.runId, gateId);
      const dependencies = [...this.dependencyEvidence(phase(this.root, this.approval).startAfter).dependencies,
        ...gate.needs.map(dep => ({ runId: this.approval.runId, gateId: dep, receiptHash: this.gateState(dep).receiptHash! }))];
      const receipt: StageReceipt = { schemaVersion: 1, kind: 'opl.development.stage.v1', runId: this.approval.runId, gateId, attempt: previous.length + 1,
        evidenceLayer: 'source', sourceSha: this.head(), approvalHash: digest(canonical(this.approval)), phaseHash, runnerHash: runnerHash(), inputHash: hash, dependencies,
        result: unchanged ? checked.status : 'failed', checkedAt: new Date().toISOString(),
        verification: { tests: checked.tests, failed: checked.failed, skipped: checked.skipped, exitCode: checked.exitCode, outputSha256: digest(checked.output),
          ...(unchanged ? checked.reason ? { reason: checked.reason } : {} : { reason: 'INPUT_CHANGED_DURING_VERIFICATION' }) } };
      appendReceipt(this.store, receipt);
      return { ...this.view(), verification: receipt.verification };
    } finally { if (snapshot) rmSync(snapshot, { recursive: true, force: true }); unlinkSync(lock); }
  }
  private allowed(path: string, write = false) {
    this.ready(); pathName(path);
    if (write) { const blocked = this.dependencyEvidence(phase(this.root, this.approval).startAfter).blockers; if (blocked.length) fail('DEPENDENCY_BLOCKED: ' + JSON.stringify(blocked)); }
    const protectedPaths = ['AGENTS.md', 'DEV_GUIDE.md', 'CONTRIBUTING.md', 'package.json', 'package-lock.json', this.approval.planPath];
    if (write && (protectedPaths.includes(path) || ['docs/', '.github/', 'tools/', 'tests/tools/'].some(p => path.startsWith(p)) ||
      this.approval.gates.some(g => g.targets?.includes(path)))) fail(`SCOPE_DENIED: protected ${path}`);
    const scope = write ? this.approval.writePaths : [...this.approval.readPaths, ...this.approval.writePaths];
    if (!matches(path, scope)) fail(`SCOPE_DENIED: ${path}`);
    return physical(this.root, path);
  }
  read(path: string) {
    this.allowed(path); const bytes = fileBytes(this.root, path);
    return { path, sha256: digest(bytes), content: bytes.toString() };
  }
  search(paths: string[], literal: string) {
    this.ready();
    if (!Array.isArray(paths) || !paths.length || paths.length > 16 || typeof literal !== 'string' || !literal || literal.length > 512) fail('bounded literal and explicit paths required');
    const files = new Set<string>();
    const walk = (path: string) => {
      const target = this.allowed(path); const stat = lstatSync(target);
      if (stat.isDirectory()) {
        if (![...this.approval.readPaths, ...this.approval.writePaths].some(p => p.endsWith('/') && matches(path, [p]))) fail(`SCOPE_DENIED: directory ${path}`);
        for (const name of readdirSync(target).sort()) walk(`${path}/${name}`);
      } else {
        if (!stat.isFile()) fail('non-regular file'); files.add(path); if (files.size > 64) fail('SEARCH_BUDGET: narrow scope');
      }
    };
    paths.forEach(p => { pathName(p); walk(p); });
    let size = 0; const hits: { path: string; line: number; text: string }[] = [];
    for (const path of files) {
      const bytes = fileBytes(this.root, path, 1024 * 1024 - size); size += bytes.length;
      if (bytes.includes(0)) fail('binary file refused');
      bytes.toString().split('\n').forEach((text, index) => { if (text.includes(literal)) hits.push({ path, line: index + 1, text }); });
      if (hits.length > 100 || Buffer.byteLength(JSON.stringify(hits)) > 32 * 1024) fail('SEARCH_BUDGET: narrow results');
    }
    return { scannedFiles: files.size, hits };
  }
  write(path: string, expectedSha256: string, content: string) {
    const target = this.allowed(path, true);
    if (typeof content !== 'string' || Buffer.byteLength(content) > 192 * 1024 || !/^(?:absent|[a-f0-9]{64})$/u.test(expectedSha256)) fail('invalid CAS write');
    const present = existsSync(target);
    if (!present && expectedSha256 !== 'absent') fail('WRITE_CONFLICT: file absent');
    const fd = openSync(target, constants.O_NOFOLLOW | (present ? constants.O_RDWR : constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL), 0o600);
    try {
      physical(this.root, path); const stat = fstatSync(fd);
      if (!stat.isFile() || stat.nlink !== 1 || stat.size > 192 * 1024) fail('invalid write target');
      if ((present ? digest(readFileSync(fd)) : 'absent') !== expectedSha256) fail('WRITE_CONFLICT: receive current bytes');
      ftruncateSync(fd, 0); const bytes = Buffer.from(content); let offset = 0;
      while (offset < bytes.length) offset += writeSync(fd, bytes, offset, bytes.length - offset, offset);
    } finally { closeSync(fd); }
    return { path, sha256: digest(content) };
  }
}

const stringSchema = { type: 'string', minLength: 1 };
const toolSpecs = [
  { name: 'dev_context', description: 'Receive current phase inputs and verified progress before repository access.', inputSchema: { type: 'object', properties: {}, additionalProperties: false } },
  { name: 'dev_read', description: 'Read one authorized file and its SHA-256.', inputSchema: { type: 'object', properties: { path: stringSchema }, required: ['path'], additionalProperties: false } },
  { name: 'dev_search', description: 'Literal search in explicit bounded paths; repository-root scanning is refused.', inputSchema: { type: 'object', properties: { paths: { type: 'array', items: stringSchema, minItems: 1, maxItems: 16 }, literal: stringSchema }, required: ['paths', 'literal'], additionalProperties: false } },
  { name: 'dev_status', description: 'Read verified stage progress; does not alter completion.', inputSchema: { type: 'object', properties: {}, additionalProperties: false } },
  { name: 'dev_verify', description: 'Request the declared acceptance gate; the host executes it and records real results.', inputSchema: { type: 'object', properties: { gateId: stringSchema }, required: ['gateId'], additionalProperties: false } },
  { name: 'dev_write', description: 'CAS write within the admitted business owner; policy and evidence are protected.', inputSchema: { type: 'object', properties: { path: stringSchema, expectedSha256: stringSchema, content: stringSchema }, required: ['path', 'expectedSha256', 'content'], additionalProperties: false } },
];
/** Unknown tools and arguments fail before any operation; completion is never a worker tool. */
export async function dispatchTool(session: DevelopmentSession, name: string, input: unknown) {
  const spec = toolSpecs.find(tool => tool.name === name); if (!spec) fail(`unknown tool: ${name}`);
  const args = object(input); keys(args, Object.keys(spec.inputSchema.properties));
  for (const key of 'required' in spec.inputSchema ? spec.inputSchema.required! : []) if (!(key in args)) fail(`missing field: ${key}`);
  switch (name) {
    case 'dev_context': return session.context();
    case 'dev_read': return session.read(args.path);
    case 'dev_search': return session.search(args.paths, args.literal);
    case 'dev_status': return session.status();
    case 'dev_verify': return session.verifyGate(args.gateId);
    case 'dev_write': return session.write(args.path, args.expectedSha256, args.content);
  }
}

/** The dedicated model worker receives only the scoped functions; prose never changes task state. */
export async function runRestrictedWorker(session: DevelopmentSession, options: { endpoint: string; model: string; maxTurns?: number; apiKey?: string }) {
  const endpoint = new URL(options.endpoint);
  if (!(endpoint.protocol === 'https:' || (endpoint.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(endpoint.hostname))) || endpoint.username || endpoint.password) fail('explicit HTTPS model endpoint required; HTTP is local-only');
  if (typeof options.model !== 'string' || !options.model.trim()) fail('model required');
  const maxTurns = options.maxTurns ?? 8; if (!Number.isSafeInteger(maxTurns) || maxTurns < 1 || maxTurns > 32) fail('invalid turn budget');
  const current = session.context();
  const messages: any[] = [{ role: 'system', content: 'You are a restricted business-owner worker. Use the admitted tools. Only the host acceptance runner advances verified progress. Source evidence never claims Candidate, Instance or production readiness. Current admission:\n' + JSON.stringify(current) },
    { role: 'user', content: 'Continue the next unfinished admitted stage. Do not restart verified work. Request its declared acceptance gate after the change.' }];
  const tools = toolSpecs.map(spec => ({ type: 'function', function: { name: spec.name, description: spec.description, parameters: spec.inputSchema } }));
  for (let turn = 0; turn < maxTurns; turn++) {
    const status = session.status(); if (status.result === 'passed') return { ...status, turns: turn };
    const response = await fetch(endpoint, { method: 'POST', headers: { 'content-type': 'application/json', ...(options.apiKey ? { authorization: `Bearer ${options.apiKey}` } : {}) },
      body: JSON.stringify({ model: options.model, messages, tools, tool_choice: 'auto', max_tokens: 2048 }), signal: AbortSignal.timeout(60_000) });
    if (!response.ok) fail(`MODEL_HTTP_${response.status}`);
    const text = await response.text(); if (Buffer.byteLength(text) > 256 * 1024) fail('model response exceeds budget');
    const body = object(JSON.parse(text)); const message = object(body.choices?.[0]?.message);
    const calls = message.tool_calls ?? []; if (!Array.isArray(calls) || calls.length > 8) fail('invalid bounded tool calls');
    messages.push({ role: 'assistant', content: typeof message.content === 'string' ? message.content : null, ...(calls.length ? { tool_calls: calls } : {}) });
    for (const call of calls) {
      if (typeof call.id !== 'string' || call.type !== 'function' || typeof call.function?.name !== 'string' || typeof call.function.arguments !== 'string') fail('invalid function call');
      let result: unknown;
      try { result = await dispatchTool(session, call.function.name, JSON.parse(call.function.arguments)); }
      catch (error: any) { result = { isError: true, reason: error.message }; }
      messages.push({ role: 'tool', tool_call_id: call.id, content: JSON.stringify(result) });
    }
    if (!calls.length) messages.push({ role: 'user', content: 'Completion is unverified. Use the admitted tools or report the concrete blocker; text cannot complete the run.' });
    if (Buffer.byteLength(JSON.stringify(messages)) > 768 * 1024) fail('worker context budget exceeded');
  }
  const status = session.status();
  return status.result === 'passed' ? { ...status, turns: maxTurns } : { ...status, result: 'blocked', reason: 'TURN_BUDGET_EXHAUSTED', turns: maxTurns };
}
/** stdio exposes only declared repository functions. Other client-native tools are not controlled by MCP. */
export async function serve(session: DevelopmentSession) {
  const lines = createInterface({ input: process.stdin, crlfDelay: Infinity }); let initialized = false;
  for await (const line of lines) {
    let request: any;
    try {
      if (Buffer.byteLength(line) > 1024 * 1024) fail('request too large');
      request = object(JSON.parse(line));
      if (request.jsonrpc !== '2.0' || typeof request.method !== 'string') fail('invalid JSON-RPC request');
      if (request.id === undefined) continue;
      let result: unknown;
      if (request.method === 'initialize') {
        if (initialized) fail('already initialized'); initialized = true;
        const versions = ['2024-11-05', '2025-03-26', '2025-06-18'];
        result = { protocolVersion: versions.includes(request.params?.protocolVersion) ? request.params.protocolVersion : versions[0], capabilities: { tools: {} }, serverInfo: { name: 'opl-development-entry', version: '2.0.0' } };
      } else if (!initialized) fail('initialize required');
      else if (request.method === 'ping') result = {};
      else if (request.method === 'tools/list') result = { tools: toolSpecs };
      else if (request.method === 'tools/call') {
        try { result = { content: [{ type: 'text', text: JSON.stringify(await dispatchTool(session, request.params?.name, request.params?.arguments ?? {})) }] }; }
        catch (error: any) { result = { isError: true, content: [{ type: 'text', text: error.message }] }; }
      } else fail('unsupported method');
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }) + '\n');
    } catch (error: any) { process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: request?.id ?? null, error: { code: -32600, message: error.message } }) + '\n'); }
  }
}
/** Verify resulting paths from signed host permissions, never from worker-modified policy. */
export function verifyWriteScope(root: string, store: string, runId: string) {
  const session = new DevelopmentSession(root, store, runId); const base = session.approval.baseSha;
  const paths = new Set([
    ...git(root, ['diff', '--name-only', '-z', '--no-renames', base, 'HEAD', '--']).split('\0'),
    ...git(root, ['diff', '--cached', '--name-only', '-z', '--no-renames', base, '--']).split('\0'),
    ...git(root, ['diff', '--name-only', '-z', '--no-renames', base, '--']).split('\0'),
    ...git(root, ['ls-files', '--others', '--exclude-standard', '-z']).split('\0'),
  ].filter(Boolean));
  for (const path of paths) { pathName(path); if (!matches(path, session.approval.writePaths)) fail(`WRITE_SCOPE_DENIED: ${path}`); }
  return { runId, baseSha: base, changedPaths: [...paths].sort() };
}
async function main() {
  const [command, storeOrApproval, runOrStore, extra, model, turns, ...rest] = process.argv.slice(2);
  const root = git(process.cwd(), ['rev-parse', '--show-toplevel']).trim();
  if (rest.length) fail('unexpected arguments');
  if (command === 'approve' && storeOrApproval && runOrStore && !extra) {
    return console.log(JSON.stringify(approveRun(root, runOrStore, JSON.parse(readFileSync(resolve(storeOrApproval), 'utf8'))), null, 2));
  }
  if (!['context', 'serve', 'verify', 'scope', 'run'].includes(command) || !storeOrApproval || !runOrStore ||
    (command === 'run' ? !extra || !model : command === 'verify' ? !extra || model || turns : extra || model || turns)) {
    fail('usage: dev-session approve <host-authorization.json> <absolute-host-store> | <context|serve|scope> <host-store> <run-id> | verify <host-store> <run-id> <gate-id> | run <host-store> <run-id> <https-chat-completions-endpoint> <model> [max-turns]');
  }
  const session = new DevelopmentSession(root, storeOrApproval, runOrStore);
  if (command === 'serve') return serve(session);
  if (command === 'scope') return console.log(JSON.stringify(verifyWriteScope(root, storeOrApproval, runOrStore), null, 2));
  if (command === 'run') {
    const result = await runRestrictedWorker(session, { endpoint: extra, model, maxTurns: turns === undefined ? undefined : Number(turns), apiKey: process.env.OPL_DEV_API_KEY });
    console.log(JSON.stringify(result, null, 2)); if (result.result !== 'passed') process.exitCode = 1; return;
  }
  const context = session.context();
  const result = command === 'context' ? context : await session.verifyGate(extra);
  console.log(JSON.stringify(result, null, 2)); if (command === 'verify' && result.result !== 'passed') process.exitCode = 1;
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  Promise.resolve().then(main).catch(error => { console.error(error.message); process.exitCode = 1; });
}

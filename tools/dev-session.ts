/** Scope-limited development tools. The host must remove unrestricted tools from the worker. */
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { closeSync, constants, existsSync, fstatSync, ftruncateSync, lstatSync, mkdirSync, openSync, readFileSync, readdirSync, realpathSync, renameSync, writeFileSync, writeSync } from 'node:fs';
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';

const digest = (value: string | Buffer) => createHash('sha256').update(value).digest('hex');
function fail(message: string): never { throw new Error(message); }
const asObject = (value: unknown): Record<string, any> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail('expected object');
  return value as Record<string, any>;
};
function exactKeys(value: Record<string, any>, keys: string[]) {
  for (const key of Object.keys(value)) if (!keys.includes(key)) fail(`unknown field: ${key}`);
}
function pathName(value: unknown, directory = false): string {
  if (typeof value !== 'string' || !value || isAbsolute(value) || value.includes('\\') || /[\x00-\x1f*?\[\]{}]/u.test(value)) fail('invalid relative path');
  const name = directory && value.endsWith('/') ? value.slice(0, -1) : value;
  if (name.split('/').some(p => !p || p === '.' || p === '..') || name === '.git' || name.startsWith('.git/') || name === '.runtime' || name.startsWith('.runtime/')) fail('invalid or protected path');
  return value;
}
interface ContextInput { path: string; startLine?: number; endLine?: number }
interface OwnerContract { primaryModules: string[]; writePaths: string[]; contractPaths: string[] }
interface DevelopmentContract { schemaVersion: 1; evidenceLayers: string[]; owners: Record<string, OwnerContract> }
interface TaskStage { id: string; owner: string; entrypoint: string; acceptance: string }
interface Task {
  schemaVersion: 2; id: string; objective: string; domainOwner: string; primaryModule: string;
  canonicalContract: string; callers: string[]; nextAction: string; blockers: string[];
  context: ContextInput[]; readPaths: string[]; writePaths: string[];
  currentState: { statusPath: string; roadmapPath: string; anchors: string[] };
  flow: { stages: TaskStage[]; terminalStage: string };
  evidence: { layer: string; receiptPath: string; statusProjection: string };
  completionEvidence: string;
}
function validateContract(raw: unknown): DevelopmentContract {
  const contract = asObject(raw);
  exactKeys(contract, ['schemaVersion', 'evidenceLayers', 'owners']);
  if (contract.schemaVersion !== 1 || !Array.isArray(contract.evidenceLayers) || !contract.evidenceLayers.length) fail('invalid development contract');
  const owners = asObject(contract.owners);
  for (const [id, value] of Object.entries(owners)) {
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(id)) fail(`invalid owner id: ${id}`);
    const owner = asObject(value); exactKeys(owner, ['primaryModules', 'writePaths', 'contractPaths']);
    for (const key of ['primaryModules', 'writePaths', 'contractPaths']) {
      if (!Array.isArray(owner[key]) || owner[key].some((item: unknown) => typeof item !== 'string' || !item)) fail(`invalid owner ${id} ${key}`);
      owner[key].forEach((item: string) => pathName(item, true));
    }
    if (!owner.primaryModules.length || !owner.contractPaths.length) fail(`owner ${id} is incomplete`);
  }
  return contract as DevelopmentContract;
}
function validateTask(raw: unknown, contract?: DevelopmentContract): Task {
  const task = asObject(raw);
  exactKeys(task, ['schemaVersion', 'id', 'objective', 'domainOwner', 'primaryModule', 'canonicalContract', 'callers', 'nextAction', 'blockers', 'context', 'readPaths', 'writePaths', 'currentState', 'flow', 'evidence', 'completionEvidence']);
  if (task.schemaVersion !== 2 || typeof task.id !== 'string' || !/^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(task.id)) fail('invalid task version or id');
  for (const key of ['objective', 'domainOwner', 'primaryModule', 'canonicalContract', 'nextAction', 'completionEvidence']) if (typeof task[key] !== 'string' || !task[key].trim()) fail(`missing ${key}`);
  for (const key of ['callers', 'blockers', 'readPaths', 'writePaths']) {
    if (!Array.isArray(task[key]) || task[key].some((item: unknown) => typeof item !== 'string' || !item)) fail(`invalid ${key}`);
  }
  if (!task.callers.length || !task.readPaths.length || !task.writePaths.length) fail('empty callers or task scope');
  [...task.readPaths, ...task.writePaths].forEach(p => pathName(p, true));
  if (!Array.isArray(task.context) || !task.context.length) fail('missing current context');
  task.context.forEach((input: unknown) => {
    const item = asObject(input);
    exactKeys(item, ['path', 'startLine', 'endLine']); pathName(item.path);
    if ((item.startLine === undefined) !== (item.endLine === undefined)) fail('context range requires both limits');
    if (item.startLine !== undefined && (!Number.isSafeInteger(item.startLine) || !Number.isSafeInteger(item.endLine) || item.startLine < 1 || item.endLine < item.startLine)) fail('invalid context range');
  });
  for (const required of ['AGENTS.md', 'docs/status.md', 'docs/roadmap.md']) {
    if (!task.context.some((item: ContextInput) => item.path === required && item.startLine === undefined)) fail(`full current input required: ${required}`);
  }
  const currentState = asObject(task.currentState); exactKeys(currentState, ['statusPath', 'roadmapPath', 'anchors']);
  if (currentState.statusPath !== 'docs/status.md' || currentState.roadmapPath !== 'docs/roadmap.md' || !Array.isArray(currentState.anchors) || !currentState.anchors.length) fail('current state must name docs/status.md and docs/roadmap.md');
  const flow = asObject(task.flow); exactKeys(flow, ['stages', 'terminalStage']);
  if (!Array.isArray(flow.stages) || flow.stages.length < 2 || typeof flow.terminalStage !== 'string') fail('incomplete development flow');
  const stageIds = new Set<string>();
  for (const rawStage of flow.stages) {
    const stage = asObject(rawStage); exactKeys(stage, ['id', 'owner', 'entrypoint', 'acceptance']);
    for (const key of ['id', 'owner', 'entrypoint', 'acceptance']) if (typeof stage[key] !== 'string' || !stage[key].trim()) fail(`invalid flow stage ${key}`);
    if (stageIds.has(stage.id)) fail(`duplicate flow stage: ${stage.id}`); stageIds.add(stage.id);
  }
  if (!stageIds.has(flow.terminalStage)) fail('terminal stage is not in flow');
  const evidence = asObject(task.evidence); exactKeys(evidence, ['layer', 'receiptPath', 'statusProjection']);
  if (typeof evidence.layer !== 'string' || typeof evidence.receiptPath !== 'string' || typeof evidence.statusProjection !== 'string') fail('invalid evidence');
  pathName(evidence.receiptPath); pathName(evidence.statusProjection);
  if (contract) {
    const owner = contract.owners[task.domainOwner]; if (!owner) fail(`unknown DDD owner: ${task.domainOwner}`);
    if (!owner.primaryModules.includes(task.primaryModule) || !owner.contractPaths.includes(task.canonicalContract)) fail('task primary module or canonical contract is not owned by domain owner');
    for (const path of task.writePaths) if (!owner.writePaths.some(root => root.endsWith('/') ? path.startsWith(root) : path === root)) fail(`write path is outside DDD owner: ${path}`);
    for (const stage of flow.stages) if (!contract.owners[stage.owner]) fail(`unknown flow owner: ${stage.owner}`);
    if (!contract.evidenceLayers.includes(evidence.layer)) fail(`unknown evidence layer: ${evidence.layer}`);
  }
  return task as Task;
}
export function parseTask(raw: unknown): Task { return validateTask(raw); }
const matches = (path: string, roots: string[]) => roots.some(root => root.endsWith('/') ? path.startsWith(root) : path === root);
const git = (root: string, args: string[]) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });

export class DevelopmentSession {
  readonly root: string;
  readonly taskPath: string;
  readonly contract: DevelopmentContract;
  private admitted?: { head: string; taskHash: string; inputs: { path: string; sha256: string }[]; task: Task };
  constructor(root: string, taskPath: string) {
    this.root = realpathSync(root); this.taskPath = pathName(taskPath);
    this.contract = this.loadContract(); this.task();
  }
  /** Refuse symlinks in every path component, including existing parent directories of new files. */
  private physical(path: string): string {
    pathName(path);
    let current = this.root;
    for (const part of path.split('/')) {
      current = resolve(current, part);
      try { if (lstatSync(current).isSymbolicLink()) fail(`symlink refused: ${path}`); }
      catch (error: any) { if (error.code !== 'ENOENT') throw error; }
    }
    const rel = relative(this.root, current);
    if (!rel || rel === '..' || rel.startsWith(`..${sep}`) || isAbsolute(rel)) fail('path escapes repository');
    return current;
  }
  private loadContract(): DevelopmentContract {
    return validateContract(JSON.parse(readFileSync(resolve(this.root, 'development/development-contract.json'), 'utf8')));
  }
  private taskBytes() { return this.fileBytes(this.taskPath, 192 * 1024); }
  private task(): Task { return validateTask(JSON.parse(this.taskBytes().toString('utf8')), this.contract); }
  private fileBytes(path: string, limit: number): Buffer {
    const target = this.physical(path);
    const fd = openSync(target, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      this.physical(path);
      const stat = fstatSync(fd);
      if (!stat.isFile() || stat.nlink !== 1 || stat.size > limit) fail(`invalid regular file or budget: ${path}`);
      return readFileSync(fd);
    } finally { closeSync(fd); }
  }
  private sameIdentity(a: string, b: string) {
    if (!existsSync(a) || !existsSync(b)) return false;
    const left = lstatSync(a); const right = lstatSync(b);
    return left.dev === right.dev && left.ino === right.ino;
  }
  private head() { return git(this.root, ['rev-parse', 'HEAD']).trim(); }
  /** Serve exact mandatory context before granting any worker file operation. This is delivery, not proof of comprehension. */
  context() {
    const taskBytes = this.taskBytes();
    const task = validateTask(JSON.parse(taskBytes.toString('utf8')), this.contract);
    const head = this.head();
    const inputs = task.context.map(input => {
      const bytes = this.fileBytes(input.path, 192 * 1024);
      const lines = bytes.toString('utf8').split('\n');
      if (input.endLine && input.endLine > lines.length) fail(`context range outside file: ${input.path}`);
      const content = input.startLine ? lines.slice(input.startLine - 1, input.endLine).join('\n') : bytes.toString('utf8');
      return { ...input, sha256: digest(bytes), content };
    });
    if (Buffer.byteLength(JSON.stringify(inputs)) > 192 * 1024) fail('required context exceeds 192 KiB; select explicit owner sections, never truncate silently');
    const taskHash = digest(taskBytes);
    if (head !== this.head() || taskHash !== digest(this.taskBytes()) ||
      inputs.some(input => digest(this.fileBytes(input.path, 192 * 1024)) !== input.sha256)) {
      this.admitted = undefined; fail('CONTEXT_STALE: inputs changed while assembling current state');
    }
    this.admitted = { head, taskHash, inputs: inputs.map(({ path, sha256 }) => ({ path, sha256 })), task: structuredClone(task) };
    return { schemaVersion: 1, evidenceLayer: 'source-observation', observedAt: new Date().toISOString(),
      taskId: task.id, objective: task.objective, dddOwner: task.domainOwner, nextAction: task.nextAction,
      blockers: task.blockers, baseSha: head, taskSha256: taskHash, context: inputs,
      readPaths: task.readPaths, writePaths: task.writePaths,
      primaryModule: task.primaryModule, canonicalContract: task.canonicalContract, callers: task.callers,
      currentState: task.currentState, flow: task.flow, evidence: task.evidence, completionEvidence: task.completionEvidence };
  }
  private ready(): Task {
    if (!this.admitted) fail('CONTEXT_REQUIRED: call dev_context first');
    const state = this.admitted;
    if (this.head() !== state.head || digest(this.taskBytes()) !== state.taskHash ||
      state.inputs.some(input => digest(this.fileBytes(input.path, 192 * 1024)) !== input.sha256)) {
      this.admitted = undefined; fail('CONTEXT_STALE: base, task, or current input changed; call dev_context again');
    }
    return state.task;
  }
  private allowed(path: string, write = false, task = this.ready()) {
    pathName(path);
    const scope = write ? task.writePaths : [...task.readPaths, ...task.writePaths, ...task.context.map(input => input.path)];
    const target = this.physical(path);
    const protectedPath = path === this.taskPath || this.sameIdentity(target, this.physical(this.taskPath)) || this.sameIdentity(target, fileURLToPath(import.meta.url)) || path === task.currentState.statusPath || path === task.currentState.roadmapPath || path === task.evidence.receiptPath || path === task.evidence.statusProjection;
    if (!matches(path, scope) || (write && protectedPath)) fail(`SCOPE_DENIED: ${path}`);
    return target;
  }
  read(path: string) {
    this.allowed(path);
    const bytes = this.fileBytes(path, 192 * 1024);
    if (bytes.length > 192 * 1024) fail('file exceeds read budget; use a separately scoped smaller input');
    return { path, sha256: digest(bytes), content: bytes.toString('utf8') };
  }
  search(paths: string[], literal: string) {
    if (!Array.isArray(paths) || !paths.length || paths.length > 16 || typeof literal !== 'string' || !literal || literal.length > 512) fail('explicit paths and bounded literal required');
    const task = this.ready();
    const files: { path: string; target: string }[] = [];
    const walk = (path: string) => {
      const target = this.allowed(path, false, task); const stat = lstatSync(target);
      if (stat.isDirectory()) {
        for (const entry of readdirSync(target).sort()) walk(`${path}/${entry}`);
      } else if (stat.isFile()) {
        files.push({ path, target }); if (files.length > 64) fail('SEARCH_BUDGET: more than 64 files; narrow paths');
      } else fail('non-regular file refused');
    };
    // Directory roots must be explicitly granted; never infer permission from descendant files.
    for (const path of paths) {
      pathName(path);
      if (existsSync(this.physical(path)) && lstatSync(this.physical(path)).isDirectory() &&
        ![...task.readPaths, ...task.writePaths].some(root => root.endsWith('/') && (path === root.slice(0, -1) || path.startsWith(root)))) fail(`SCOPE_DENIED: directory ${path}`);
      // A directory grant covers its root for traversal, but not any sibling or parent.
      if (lstatSync(this.physical(path)).isDirectory()) {
        for (const entry of readdirSync(this.physical(path)).sort()) walk(`${path}/${entry}`);
      } else walk(path);
    }
    const hits: { path: string; line: number; text: string }[] = [];
    let scannedBytes = 0;
    for (const { path, target } of files) {
      const stat = lstatSync(target);
      if (stat.nlink !== 1 || stat.size > 1024 * 1024 - scannedBytes) fail('SEARCH_BUDGET: invalid file or more than 1 MiB; narrow paths');
      const bytes = this.fileBytes(path, 1024 * 1024 - scannedBytes); scannedBytes += bytes.length;
      if (scannedBytes > 1024 * 1024) fail('SEARCH_BUDGET: more than 1 MiB; narrow paths');
      if (bytes.includes(0)) fail(`binary file refused: ${path}`);
      bytes.toString('utf8').split('\n').forEach((text, index) => {
        if (text.includes(literal)) hits.push({ path, line: index + 1, text });
      });
      if (hits.length > 100 || Buffer.byteLength(JSON.stringify(hits)) > 32 * 1024) fail('SEARCH_BUDGET: result too large; narrow literal or paths');
    }
    return { scannedFiles: files.length, hits };
  }
  write(path: string, expectedSha256: string, content: string) {
    const target = this.allowed(path, true);
    if (typeof content !== 'string' || Buffer.byteLength(content) > 192 * 1024) fail('invalid write content or budget');
    if (typeof expectedSha256 !== 'string' || !/^(?:absent|[a-f0-9]{64})$/u.test(expectedSha256)) fail('expectedSha256 required');
    if (existsSync(target)) {
      const stat = lstatSync(target);
      if (!stat.isFile() || stat.nlink !== 1 || stat.size > 192 * 1024) fail('invalid write target or budget');
    }
    // Open without following the final symlink, and compare/write the same inode.
    if (!lstatSync(dirname(target)).isDirectory()) fail('missing parent directory');
    const present = existsSync(target);
    if (!present && expectedSha256 !== 'absent') fail('WRITE_CONFLICT: file is absent');
    const fd = openSync(target, constants.O_NOFOLLOW | (present ? constants.O_RDWR : constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL), 0o600);
    try {
      this.physical(path);
      const stat = fstatSync(fd);
      if (!stat.isFile() || stat.nlink !== 1 || stat.size > 192 * 1024) fail('invalid write target or budget');
      const current = present ? digest(readFileSync(fd)) : 'absent';
      if (current !== expectedSha256) fail('WRITE_CONFLICT: read current bytes before editing');
      ftruncateSync(fd, 0);
      // readFileSync advanced the descriptor offset: use an explicit position.
      const bytes = Buffer.from(content);
      let offset = 0;
      while (offset < bytes.length) offset += writeSync(fd, bytes, offset, bytes.length - offset, offset);
    } finally { closeSync(fd); }
    return { path, sha256: digest(content) };
  }
}

/** Check worktree changes against a trusted task scope; read behavior cannot be audited by Git. */
export function verifyWriteScope(root: string, taskPath: string, base: string) {
  pathName(taskPath);
  if (!/^[a-f0-9]{40}(?:[a-f0-9]{24})?$/u.test(base)) fail('exact approved base SHA required');
  // The worker's edited policy is never an authority for its own change.
  const baseSha = git(root, ['rev-parse', '--verify', `${base}^{commit}`]).trim();
  const contractPath = resolve(root, 'development/development-contract.json');
  const contract = validateContract(JSON.parse(readFileSync(contractPath, 'utf8')));
  const task = validateTask(JSON.parse(git(root, ['show', `${baseSha}:${taskPath}`])), contract);
  const paths = new Set([
    ...git(root, ['diff', '--name-only', '-z', '--no-renames', baseSha, 'HEAD', '--']).split('\0'),
    ...git(root, ['diff', '--cached', '--name-only', '-z', '--no-renames', baseSha, '--']).split('\0'),
    ...git(root, ['diff', '--name-only', '-z', '--no-renames', baseSha, '--']).split('\0'),
    ...git(root, ['ls-files', '--others', '--exclude-standard', '-z']).split('\0'),
  ].filter(Boolean));
  for (const path of paths) {
    pathName(path);
    if (path === taskPath || !matches(path, task.writePaths)) fail(`WRITE_SCOPE_DENIED: ${path}`);
  }
  return { base: baseSha, taskId: task.id, changedPaths: [...paths].sort() };
}


function atomicJson(path: string, value: unknown) {
  const temp = `${path}.tmp-${process.pid}`; writeFileSync(temp, JSON.stringify(value, null, 2) + '\n', { mode: 0o600 }); renameSync(temp, path);
}
function generatedStateBlock(state: any) {
  const rows = state.tasks.map((task: any) => `| ${task.taskId} | ${task.domainOwner} | ${task.result} | [receipt](${task.receiptPath.startsWith('docs/') ? task.receiptPath.slice(5) : task.receiptPath}) | \`${task.sourceSha}\` |`).join('\n');
  return [
    '<!-- BEGIN GENERATED DEVELOPMENT STATE -->', '',
    '| Task | DDD owner | State | Evidence | Source SHA |',
    '| --- | --- | --- | --- | --- |', rows, '',
    'This block is generated from `development/state/current.json`; it records development evidence only and does not claim runtime or production readiness.', '',
    '<!-- END GENERATED DEVELOPMENT STATE -->'
  ].join('\n');
}
function replaceGenerated(path: string, block: string) {
  const source = readFileSync(path, 'utf8'); const begin = '<!-- BEGIN GENERATED DEVELOPMENT STATE -->'; const end = '<!-- END GENERATED DEVELOPMENT STATE -->';
  const start = source.indexOf(begin); const finish = source.indexOf(end);
  const next = start >= 0 && finish > start ? source.slice(0, start) + block + source.slice(finish + end.length) : `${source.trimEnd()}\n\n${block}\n`;
  writeFileSync(path, next);
}
export function publishReceipt(root: string, taskPath: string, receipt: unknown) {
  const session = new DevelopmentSession(root, taskPath); const task = session.context(); const value = asObject(receipt);
  exactKeys(value, ['schemaVersion', 'taskId', 'sourceSha', 'result', 'evidenceLayer', 'checkedAt', 'commands', 'changedPaths', 'summary']);
  if (value.schemaVersion !== 1 || value.taskId !== task.taskId || value.sourceSha !== task.baseSha || !['passed', 'blocked', 'failed'].includes(value.result) || value.evidenceLayer !== task.evidence.layer || typeof value.checkedAt !== 'string' || typeof value.summary !== 'string' || !Array.isArray(value.commands) || !Array.isArray(value.changedPaths)) fail('invalid completion receipt');
  for (const path of value.changedPaths) if (typeof path !== 'string' || !task.writePaths.some((root: string) => root.endsWith('/') ? path.startsWith(root) : path === root)) fail(`receipt changed path outside task scope: ${path}`);
  const receiptPath = resolve(root, task.evidence.receiptPath); mkdirSync(dirname(receiptPath), { recursive: true }); atomicJson(receiptPath, value);
  const statePath = resolve(root, task.evidence.statusProjection); mkdirSync(dirname(statePath), { recursive: true });
  const state = { schemaVersion: 1, generatedAt: new Date().toISOString(), sourceSha: value.sourceSha, tasks: [{ taskId: value.taskId, domainOwner: task.dddOwner, result: value.result, receiptPath: task.evidence.receiptPath, sourceSha: value.sourceSha, checkedAt: value.checkedAt }] };
  atomicJson(statePath, state); const block = generatedStateBlock(state);
  replaceGenerated(resolve(root, task.currentState.statusPath), block); replaceGenerated(resolve(root, task.currentState.roadmapPath), block);
  return { receiptPath: task.evidence.receiptPath, statusProjection: task.evidence.statusProjection, result: value.result };
}
const stringSchema = { type: 'string' };
const toolSpecs = [
  { name: 'dev_context', description: 'Receive mandatory current evidence, exact source hashes, next action and authorized scope before file operations.', inputSchema: { type: 'object', properties: {}, additionalProperties: false } },
  { name: 'dev_read', description: 'Read one authorized repository file and its SHA-256.', inputSchema: { type: 'object', properties: { path: stringSchema }, required: ['path'], additionalProperties: false } },
  { name: 'dev_search', description: 'Search a literal within explicit authorized paths, up to 64 files. Repository-root scanning is not allowed.', inputSchema: { type: 'object', properties: { paths: { type: 'array', items: stringSchema, minItems: 1, maxItems: 16 }, literal: stringSchema }, required: ['paths', 'literal'], additionalProperties: false } },
  { name: 'dev_write', description: 'Replace one authorized file using the SHA-256 read from its current bytes, or absent for a new file.', inputSchema: { type: 'object', properties: { path: stringSchema, expectedSha256: stringSchema, content: stringSchema }, required: ['path', 'expectedSha256', 'content'], additionalProperties: false } },
];
export function dispatchTool(session: DevelopmentSession, name: string, input: unknown) {
  const args = asObject(input);
  const spec = toolSpecs.find(tool => tool.name === name);
  if (!spec) fail(`unknown tool: ${name}`);
  exactKeys(args, Object.keys(spec.inputSchema.properties));
  for (const required of 'required' in spec.inputSchema ? spec.inputSchema.required! : []) if (!(required in args)) fail(`missing field: ${required}`);
  switch (name) {
    case 'dev_context': return session.context();
    case 'dev_read': return session.read(args.path);
    case 'dev_search': return session.search(args.paths, args.literal);
    case 'dev_write': return session.write(args.path, args.expectedSha256, args.content);
  }
}
/** Minimal stdio MCP server; no shell, process, network or unrestricted filesystem tool is exposed. */
export async function serve(session: DevelopmentSession) {
  const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });
  let initialized = false;
  for await (const line of lines) {
    let request: any;
    try {
      if (Buffer.byteLength(line) > 1024 * 1024) fail('request too large');
      request = JSON.parse(line);
      if (request.jsonrpc !== '2.0' || typeof request.method !== 'string') fail('invalid JSON-RPC request');
      if (request.id === undefined) continue;
      let result: unknown;
      if (request.method === 'initialize') {
        const supported = ['2024-11-05', '2025-03-26', '2025-06-18'];
        initialized = true;
        result = { protocolVersion: supported.includes(request.params?.protocolVersion) ? request.params.protocolVersion : '2024-11-05', capabilities: { tools: {} }, serverInfo: { name: 'opl-development-scope', version: '1.0.0' } };
      } else if (!initialized) fail('initialize required');
      else if (request.method === 'ping') result = {};
      else if (request.method === 'tools/list') result = { tools: toolSpecs };
      else if (request.method === 'tools/call') {
        try { result = { content: [{ type: 'text', text: JSON.stringify(dispatchTool(session, request.params?.name, request.params?.arguments ?? {})) }] }; }
        catch (error: any) { result = { isError: true, content: [{ type: 'text', text: error.message }] }; }
      } else fail(`unsupported method: ${request.method}`);
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }) + '\n');
    } catch (error: any) {
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: request?.id ?? null, error: { code: -32600, message: error.message } }) + '\n');
    }
  }
}

function main() {
  const [command, task, base, ...extra] = process.argv.slice(2);
  if (!task || extra.length || !['context', 'serve', 'verify', 'publish'].includes(command) || ((command === 'verify' || command === 'publish') ? !base : base)) fail('usage: node tools/dev-session.ts <context|serve|verify|publish> <task.json> [receipt.json|exact-base-sha]');
  const root = git(process.cwd(), ['rev-parse', '--show-toplevel']).trim();
  if (command === 'verify') console.log(JSON.stringify(verifyWriteScope(root, task, base), null, 2));
  else if (command === 'publish') { const receipt = JSON.parse(readFileSync(resolve(root, base), 'utf8')); console.log(JSON.stringify(publishReceipt(root, task, receipt), null, 2)); }
  else if (command === 'context') {
    const state = new DevelopmentSession(root, task).context();
    const directory = resolve(root, '.runtime/development'); mkdirSync(directory, { recursive: true });
    const target = resolve(directory, `${state.taskId}.json`); writeFileSync(target, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 });
    console.log(JSON.stringify({ taskId: state.taskId, baseSha: state.baseSha, stateFile: target, nextAction: state.nextAction }, null, 2));
  } else return serve(new DevelopmentSession(root, task));
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  Promise.resolve().then(main).catch(error => { console.error(error.message); process.exitCode = 1; });
}

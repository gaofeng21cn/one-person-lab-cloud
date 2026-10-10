#!/usr/bin/env node
/**
 * Host-side thin adapter between the owner-slice framework artifacts and the
 * existing Cloud development entry.
 *
 * It does not implement another runner, host store or ownership registry. It
 * validates a framework slice manifest with the framework's own validator,
 * converts it into the existing Cloud host admission record (`RunApproval`),
 * resolves a downstream `requires` binding against a host store by receipt
 * hash, and checks host stage receipts against the framework's stage-receipt
 * and receipt-reference contracts. The trusted host runs this file; nothing
 * here writes the host store.
 */
import { createHash } from 'node:crypto';
import { existsSync, lstatSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { isAbsolute, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

type Json = unknown;
type Validation = { valid: boolean; code?: string; detail?: string };

export const digest = (value: string | Buffer) => createHash('sha256').update(value).digest('hex');

/** Canonical JSON identical to the host's receipt addressing. */
export function canonical(value: Json): string {
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (value && typeof value === 'object') {
    return '{' + Object.keys(value as Record<string, Json>).sort()
      .map(key => JSON.stringify(key) + ':' + canonical((value as Record<string, Json>)[key])).join(',') + '}';
  }
  return JSON.stringify(value);
}

function fail(message: string): never { throw new Error(message); }

function object(value: Json, label: string): Record<string, any> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail(`${label} must be an object`);
  return value as Record<string, any>;
}

function identifier(value: Json, label: string): string {
  if (typeof value !== 'string' || !/^[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)*$/u.test(value)) fail(`${label} must be an identifier`);
  return value;
}

function sha256(value: Json, label: string): string {
  if (typeof value !== 'string' || !/^[a-f0-9]{64}$/u.test(value)) fail(`${label} must be 64-hex`);
  return value;
}

function repoPath(value: Json, label: string, directoryAllowed: boolean): string {
  if (typeof value !== 'string' || !value || isAbsolute(value) || value.includes('\\') || /[\x00-\x1f*?\[\]{}]/u.test(value)) {
    fail(`${label} must be a repository-relative path`);
  }
  const trimmed = directoryAllowed && value.endsWith('/') ? value.slice(0, -1) : value;
  if (!trimmed || trimmed.split('/').some(part => !part || part === '.' || part === '..')) fail(`${label} must be a repository-relative path`);
  return value;
}

export interface FrameValidators {
  validateSliceManifest: (record: Json) => Validation;
  validateStageReceipt: (record: Json) => Validation;
  validateReceiptReference: (record: Json, options?: { receipts?: Map<string, Json> }) => Validation;
}

const FRAMEWORK_VALIDATORS = [
  ['validators/lib/manifest.mjs', 'validateSliceManifest'],
  ['validators/lib/stage-receipt.mjs', 'validateStageReceipt'],
  ['validators/lib/receipt-reference.mjs', 'validateReceiptReference'],
] as const;

/** Load the framework's own validators; a missing module is refused, never stubbed. */
export async function loadFrameValidators(frameworkRoot: string): Promise<FrameValidators> {
  if (typeof frameworkRoot !== 'string' || !isAbsolute(frameworkRoot)) fail('framework root must be an absolute path');
  const loaded: Record<string, any> = {};
  for (const [relative, exported] of FRAMEWORK_VALIDATORS) {
    const file = resolve(frameworkRoot, relative);
    if (!existsSync(file) || !statSync(file).isFile()) fail(`framework validator missing: ${relative}`);
    const module = await import(pathToFileURL(file).href);
    if (typeof module[exported] !== 'function') fail(`framework validator export missing: ${relative}#${exported}`);
    loaded[exported] = module[exported];
  }
  return {
    validateSliceManifest: loaded.validateSliceManifest,
    validateStageReceipt: loaded.validateStageReceipt,
    validateReceiptReference: loaded.validateReceiptReference,
  };
}

function readJson(path: string, label: string): Json {
  if (!existsSync(path) || !lstatSync(path).isFile()) fail(`${label} missing: ${path}`);
  return JSON.parse(readFileSync(path, 'utf8'));
}

function runDirectory(store: string, runId: string) { return join(store, 'runs', runId); }

/** The attempt sequence of one named gate, addressed by the same hash the host records. */
function receiptEntries(store: string, runId: string, gateId: string): { hash: string; receipt: Record<string, any>; path: string }[] {
  const directory = join(runDirectory(store, runId), 'receipts');
  if (!existsSync(directory)) return [];
  // Only the named gate's attempt files are opened or parsed; an unrelated
  // gate's record (including a malformed one) never influences resolution.
  const attempt = new RegExp(`^${gateId.replace(/[.*+?^${}()|[\]\\]/gu, '\\$&')}-([1-9][0-9]*)\\.json$`, 'u');
  const entries: { hash: string; receipt: Record<string, any>; path: string }[] = [];
  for (const name of readdirSync(directory).sort()) {
    if (!attempt.test(name)) continue;
    const path = join(directory, name);
    if (!lstatSync(path).isFile()) continue;
    const receipt = object(JSON.parse(readFileSync(path, 'utf8')), 'stage receipt');
    entries.push({ hash: digest(canonical(receipt)), receipt, path });
  }
  return entries;
}

/**
 * Resolve a manifest `requires` list against a host store: every entry must be
 * an already-approved upstream run whose recorded receipt content hashes to
 * the declared receiptHash and is passed. Returns the host admission's
 * requires map, keyed by the upstream phase selection id.
 */
export function resolveRequires(manifest: Record<string, any>, store: string): Record<string, { runId: string; gateId: string }> {
  const entries = manifest.requires;
  if (entries === undefined || (Array.isArray(entries) && entries.length === 0)) return {};
  if (!Array.isArray(entries)) fail('requires must be an array');
  if (typeof store !== 'string' || !isAbsolute(store)) fail('absolute host store required to resolve requires');
  const requires: Record<string, { runId: string; gateId: string }> = {};
  for (const raw of entries) {
    const entry = object(raw, 'requires entry');
    const runId = identifier(entry.runId, 'requires runId');
    const gateId = identifier(entry.gateId, 'requires gateId');
    const receiptHash = sha256(entry.receiptHash, 'requires receiptHash');
    const approval = object(readJson(join(runDirectory(store, runId), 'approval.json'), `approved run ${runId}`), 'upstream approval');
    const selectionId = identifier(object(approval.selection, 'upstream selection').id, 'upstream selection id');
    const found = receiptEntries(store, runId, gateId).find(candidate => candidate.hash === receiptHash);
    if (!found) fail(`EVIDENCE_INVALID: requires receipt hash is not present in the host store for ${runId}/${gateId}`);
    if (found.receipt.runId !== runId || found.receipt.gateId !== gateId) fail(`EVIDENCE_INVALID: requires receipt identity mismatch for ${runId}/${gateId}`);
    if (found.receipt.result !== 'passed') fail(`UPSTREAM_NOT_PASSED: required receipt ${runId}/${gateId} is ${found.receipt.result}`);
    if (requires[selectionId]) fail(`EVIDENCE_INVALID: duplicate requires binding for ${selectionId}`);
    requires[selectionId] = { runId, gateId };
  }
  return requires;
}

/** Consume one framework receipt reference against the host store and the framework contract. */
export function consumeReceiptReference(validators: FrameValidators, store: string, reference: Json) {
  const ref = object(reference, 'receipt reference');
  const runId = identifier(ref.runId, 'reference runId');
  const gateId = identifier(ref.gateId, 'reference gateId');
  if (!Number.isInteger(ref.attempt) || ref.attempt < 1) fail('reference attempt must be an integer >= 1');
  const receiptHash = sha256(ref.receiptHash, 'reference receiptHash');
  const entries = receiptEntries(store, runId, gateId);
  const found = entries.find(candidate => candidate.hash === receiptHash);
  if (!found) fail(`EVIDENCE_INVALID: receipt hash is not present in the host store for ${runId}/${gateId}`);
  if (found.receipt.runId !== runId || found.receipt.gateId !== gateId || found.receipt.attempt !== ref.attempt) {
    fail('EVIDENCE_INVALID: receipt identity mismatch for the declared reference');
  }
  const stage = validators.validateStageReceipt(found.receipt);
  if (!stage.valid) fail(`EVIDENCE_INVALID: ${stage.code ?? 'SCHEMA_INVALID'}${stage.detail ? ': ' + stage.detail : ''}`);
  const receipts = new Map(entries.map(entry => [entry.hash, entry.receipt]));
  const binding = validators.validateReceiptReference(ref, { receipts });
  if (!binding.valid) fail(`${binding.code ?? 'SCHEMA_INVALID'}${binding.detail ? ': ' + binding.detail : ''}`);
  return { runId, gateId, attempt: ref.attempt, receiptHash: found.hash, receipt: found.receipt };
}

/** Check one host stage receipt against the framework's stage-receipt contract. */
export function checkStageReceipt(validators: FrameValidators, store: string, runId: string, gateId: string, attempt: number) {
  const run = identifier(runId, 'runId');
  const gate = identifier(gateId, 'gateId');
  if (!Number.isInteger(attempt) || attempt < 1) fail('attempt must be an integer >= 1');
  const path = join(runDirectory(store, run), 'receipts', `${gate}-${attempt}.json`);
  const receipt = object(readJson(path, 'stage receipt'), 'stage receipt');
  const validation = validators.validateStageReceipt(receipt);
  return { path, receiptHash: digest(canonical(receipt)), valid: validation.valid, code: validation.code, detail: validation.detail, result: receipt.result, verification: receipt.verification };
}

export interface ConvertOptions {
  manifestPath: string;
  gatesPath: string;
  frameworkRoot: string;
  store: string;
  planPath: string;
  coauthorRuns?: string[];
}

/**
 * Convert a framework slice manifest into the existing Cloud host admission
 * record. Host-only: the record is returned, never written; `dev:approve`
 * remains the only admission writer.
 */
export async function convertManifest(options: ConvertOptions) {
  const opts = object(options, 'convert options');
  const manifestPath = typeof opts.manifestPath === 'string' && isAbsolute(opts.manifestPath) ? opts.manifestPath : fail('absolute manifestPath required');
  const gatesPath = typeof opts.gatesPath === 'string' && isAbsolute(opts.gatesPath) ? opts.gatesPath : fail('absolute gatesPath required');
  const manifest = object(readJson(manifestPath, 'slice manifest'), 'slice manifest');
  const validators = await loadFrameValidators(opts.frameworkRoot);
  const validation = validators.validateSliceManifest(manifest);
  if (!validation.valid) fail(`${validation.code ?? 'SCHEMA_INVALID'}${validation.detail ? ': ' + validation.detail : ''}`);

  const sliceId = identifier(manifest.sliceId, 'sliceId');
  const owner = identifier(manifest.owner, 'owner');
  const selection = object(manifest.selection, 'selection');
  if (Object.keys(selection).length !== 2) fail('selection must be exactly {collection, id}');
  if (!['workPackages', 'executionSlices', 'parallelPreparation'].includes(selection.collection)) fail('selection.collection is not an admitted phase collection');
  const selectionId = identifier(selection.id, 'selection.id');
  const baseline = object(manifest.baseline, 'baseline');
  if (typeof baseline.repo !== 'string' || !baseline.repo) fail('baseline.repo is required');
  if (!/^[a-f0-9]{40}$/u.test(baseline.sha)) fail('baseline.sha must be 40-hex');
  const planPath = repoPath(opts.planPath, 'planPath', false);

  if (!Array.isArray(manifest.writeSet) || !manifest.writeSet.length) fail('writeSet must be a non-empty array');
  const writePaths = manifest.writeSet.map((path: Json, index: number) => repoPath(path, `writeSet[${index}]`, true));
  if (!Array.isArray(manifest.readPaths)) fail('readPaths must be an array');
  const readPaths = manifest.readPaths.map((path: Json, index: number) => repoPath(path, `readPaths[${index}]`, true));

  if (!Array.isArray(manifest.gates) || !manifest.gates.length) fail('gates must be a non-empty array');
  const gateIds = manifest.gates.map((gate: Json, index: number) => identifier(gate, `gates[${index}]`));
  if (new Set(gateIds).size !== gateIds.length) fail('gates must be non-empty unique identifiers');
  const rawDefinitions = readJson(gatesPath, 'gate definitions');
  if (!Array.isArray(rawDefinitions)) fail('gate definitions must be an array');
  const definitions = new Map<string, Record<string, any>>();
  for (const raw of rawDefinitions) {
    const definition = object(raw, 'gate definition');
    for (const key of Object.keys(definition)) {
      if (!['id', 'kind', 'inputs', 'targets', 'cwd', 'database', 'needs'].includes(key)) fail(`unknown gate field: ${key}`);
    }
    const gateId = identifier(definition.id, 'gate id');
    if (definitions.has(gateId)) fail(`duplicate gate definition: ${gateId}`);
    if (!['node', 'go', 'browser', 'generated', 'developmentPlan'].includes(definition.kind)) fail(`unknown acceptance runner: ${definition.kind}`);
    if (!Array.isArray(definition.inputs) || !definition.inputs.length) fail(`gate inputs required: ${gateId}`);
    definition.inputs.forEach((path: Json, index: number) => repoPath(path, `gate ${gateId} inputs[${index}]`, true));
    if (!Array.isArray(definition.needs)) fail(`gate needs must be an array: ${gateId}`);
    definition.needs.forEach((need: Json, index: number) => identifier(need, `gate ${gateId} needs[${index}]`));
    if (definition.kind === 'node') {
      if (!Array.isArray(definition.targets) || !definition.targets.length) fail(`Node gates require explicit test targets: ${gateId}`);
      definition.targets.forEach((target: Json, index: number) => repoPath(target, `gate ${gateId} targets[${index}]`, false));
    } else if (definition.targets !== undefined) fail('targets only apply to node tests');
    if (definition.kind === 'go') repoPath(definition.cwd, 'gate cwd', true);
    else if (definition.cwd !== undefined) fail('cwd only applies to Go checks');
    // The host-owned isolated fixture declaration travels through the shared
    // converter unchanged; the host admission re-validates it before approval.
    if (definition.database !== undefined) {
      if (definition.database !== 'isolated-owner-postgres') fail(`unknown isolated database fixture: ${gateId}`);
      if (definition.kind !== 'go') fail(`isolated database fixture only applies to Go checks: ${gateId}`);
    }
    definitions.set(gateId, definition);
  }
  for (const gateId of gateIds) if (!definitions.has(gateId)) fail(`gate definitions do not cover the manifest gates: ${gateId}`);
  for (const gateId of definitions.keys()) if (!gateIds.includes(gateId)) fail(`gate definitions do not match the manifest gates: ${gateId}`);

  const requires = resolveRequires(manifest, opts.store);
  const approval: Record<string, any> = {
    schemaVersion: 1,
    runId: sliceId,
    baseSha: baseline.sha,
    planPath,
    selection: { collection: selection.collection, id: selectionId },
    owner,
    readPaths,
    writePaths,
    gates: gateIds.map(gateId => definitions.get(gateId)),
    requires,
  };
  if (opts.coauthorRuns !== undefined) {
    if (!Array.isArray(opts.coauthorRuns)) fail('coauthorRuns must be an array');
    for (const [index, runId] of opts.coauthorRuns.entries()) {
      const value = identifier(runId, `coauthorRuns[${index}]`);
      if (value === sliceId) fail('self coauthor refused');
    }
    approval.coauthorRuns = opts.coauthorRuns;
  }
  return approval;
}

async function main() {
  const [command, ...args] = process.argv.slice(2);
  if (command === 'convert') {
    const [manifestPath, gatesPath, frameworkRoot, store, planPath, ...rest] = args;
    let coauthorRuns: string[] | undefined;
    if (rest.length) {
      if (rest.length !== 2 || rest[0] !== '--coauthors') fail('usage: convert <manifest> <gates> <frameworkRoot> <store> <planPath> [--coauthors a,b]');
      coauthorRuns = rest[1].split(',').filter(Boolean);
    }
    return console.log(JSON.stringify(await convertManifest({ manifestPath, gatesPath, frameworkRoot, store, planPath, coauthorRuns }), null, 2));
  }
  if (command === 'resolve') {
    const [manifestPath, store] = args;
    const manifest = object(readJson(resolve(manifestPath), 'slice manifest'), 'slice manifest');
    return console.log(JSON.stringify(resolveRequires(manifest, store), null, 2));
  }
  if (command === 'consume') {
    const [store, referencePath, frameworkRoot] = args;
    const validators = await loadFrameValidators(frameworkRoot);
    return console.log(JSON.stringify(consumeReceiptReference(validators, store, readJson(resolve(referencePath), 'receipt reference')), null, 2));
  }
  if (command === 'check-receipt') {
    const [store, runId, gateId, attempt, frameworkRoot] = args;
    const validators = await loadFrameValidators(frameworkRoot);
    return console.log(JSON.stringify(checkStageReceipt(validators, store, runId, gateId, Number(attempt)), null, 2));
  }
  fail('usage: dev-frame-bridge <convert|resolve|consume|check-receipt> ...');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  Promise.resolve().then(main).catch(error => { console.error(error.message); process.exitCode = 1; });
}

/**
 * Machine check for the pull-request governance entry. The PR body declares the
 * claim; the trusted host store, a signed host proof and the canonical owner
 * remain the evidence owners. A PR body is never a receipt, and an author's
 * fields or files never become the trust root.
 */
import { execFileSync } from 'node:child_process';
import { createHash, createPublicKey, verify } from 'node:crypto';
import { existsSync, readFileSync, realpathSync, statSync } from 'node:fs';
import { dirname, isAbsolute, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  authorizePhaseWriteScope, canonical, digest, gateInputHash, openTrustedStore, parseApproval, parseStageReceipt,
  readRunApproval, receiptEvidenceByHash, resolveApprovedPhase, resolvePhaseRecord, stageReceiptEvidence, currentRunnerHash,
  type PhaseSelection, type RunApproval, type StageReceipt,
} from './dev-session.ts';

export const requiredSections = [
  'Decision Conclusion',
  'Current Problem',
  'Business SSOT',
  'Development SSOT',
  'Baseline',
  'Ownership',
  'Write Set',
  'Receipt Pipeline',
  'Acceptance Criteria',
  'Verification',
  'Limitations',
  'Terminal State',
  'Merge Danger',
] as const;

export type ReceiptType = 'source-check' | 'development-stage' | 'business' | 'instance';
export type ReceiptResult = 'passed' | 'failed' | 'pending';
export type TerminalState = 'merge-ready' | 'source-complete' | 'blocked';
export type GovernanceMode = 'record' | 'merge';

export interface ReceiptClaim { type: ReceiptType; result: ReceiptResult; fields: Record<string, string> }
interface ParsedReceipt extends ReceiptClaim { complete: boolean }
export interface GovernanceClaim {
  baseSha: string; dddOwner: string; phase: string; writeSet: string[];
  receipts: ReceiptClaim[]; terminalState: TerminalState; mergeDanger: string;
}
export interface MergeAuthorization {
  claimed: boolean; ok: boolean; basis?: 'host-store' | 'signed-proof'; runId?: string; gateId?: string; attempt?: number;
  receiptDigest?: string; errors: string[]; checkedGates?: { gateId: string; receiptHash: string }[];
}
export interface GovernanceFacts {
  plan?: unknown;
  changedPaths?: readonly string[];
  eventBaseSha?: string;
  headSha?: string;
  root?: string;
  /** `record` checks the governance record; `merge` additionally requires a verified host acceptance. */
  mode?: GovernanceMode;
  /** An absolute trusted host store for local verification; never a repository path. */
  hostStore?: string;
  /** The trusted host authority public key (PEM or base64 PEM); never taken from the pull request. */
  authorityPublicKey?: string;
}
export interface GovernanceResult { ok: boolean; errors: string[]; claim?: GovernanceClaim; authorization: MergeAuthorization }

const receiptTypes: ReceiptType[] = ['source-check', 'development-stage', 'business', 'instance'];
const receiptResults: ReceiptResult[] = ['passed', 'failed', 'pending'];
const receiptFields: Record<ReceiptType, string[]> = {
  'source-check': ['path'],
  'development-stage': ['path', 'run', 'gate', 'attempt', 'sha256', 'proof'],
  business: ['authority', 'ref'],
  instance: ['ref'],
};
const receiptPathFor = (run: string, gate: string, attempt: string) => `runs/${run}/receipts/${gate}-${attempt}.json`;
const identifier = /^[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)*$/u;
const sha40 = /^[a-f0-9]{40}$/u;
const sha256hex = /^[a-f0-9]{64}$/u;
const sourceCheckReceiptPattern = /^docs\/evidence\/source-checks\/[A-Za-z0-9._-]+\.json$/u;
const proofPattern = /^docs\/evidence\/development-stage\/[A-Za-z0-9._-]+\.json$/u;

interface Section { name: string; content: string }
function sections(body: string): Section[] {
  const result: Section[] = [];
  let current: { name: string; lines: string[] } | undefined;
  for (const line of body.replace(/\r\n?/gu, '\n').split('\n')) {
    const heading = /^## (.+?)\s*$/u.exec(line);
    if (heading) { if (current) result.push({ name: current.name, content: current.lines.join('\n') }); current = { name: heading[1], lines: [] }; continue; }
    if (/^#(?!#)/u.test(line)) { if (current) { result.push({ name: current.name, content: current.lines.join('\n') }); current = undefined; } continue; }
    current?.lines.push(line);
  }
  if (current) result.push({ name: current.name, content: current.lines.join('\n') });
  return result;
}
const withoutComments = (content: string) => content.replace(/<!--[\s\S]*?-->/gu, '');
const text = (content: string) => withoutComments(content).trim();
const lines = (content: string) => withoutComments(content).split('\n').map((line) => line.trim()).filter(Boolean);
const bullets = (content: string) => lines(content).filter((line) => /^[-*]\s+/u.test(line));
function labeled(content: string, label: string): string | undefined {
  const pattern = new RegExp(`^(?:[-*]\\s*)?${label}:\\s*(.+)$`, 'iu');
  for (const line of lines(content)) { const match = pattern.exec(line); if (match) return match[1].trim(); }
  return undefined;
}
const unquote = (value: string) => { const match = /^`([^`]+)`$/u.exec(value); return match ? match[1] : value; };
function relativePath(value: string): boolean {
  if (!value || isAbsolute(value) || value.includes('\\') || /[\x00-\x1f*?\[\]{}]/u.test(value)) return false;
  const parts = value.replace(/\/$/u, '').split('/');
  if (!parts.length || parts.some((part) => !part || part === '.' || part === '..')) return false;
  return !parts.some((part) => part === '.git' || part === 'node_modules');
}
const covers = (declaration: string, path: string) =>
  declaration.endsWith('/') ? path.startsWith(declaration) : path === declaration;
function repositoryPath(root: string, path: string) {
  const target = resolve(root, path);
  const rel = relative(root, target);
  if (!rel || rel === '..' || rel.startsWith('..' + sep) || isAbsolute(rel)) return undefined;
  return resolve(realpathSync(root), rel);
}
function decodeAuthority(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const trimmed = value.trim();
  if (trimmed.includes('-----BEGIN')) return trimmed;
  try {
    const decoded = Buffer.from(trimmed, 'base64').toString('utf8');
    return decoded.includes('-----BEGIN') ? decoded : undefined;
  } catch { return undefined; }
}

/** Check one pull-request body against the plan, the git change scope, receipts and a trusted host proof. */
export function checkPullRequestBody(body: string, facts: GovernanceFacts = {}): GovernanceResult {
  const errors: string[] = [];
  const mode: GovernanceMode = facts.mode ?? 'record';
  const authorization: MergeAuthorization = { claimed: false, ok: false, errors: [] };
  const found = sections(body);
  for (const name of requiredSections) {
    const matches = found.filter((section) => section.name === name);
    if (!matches.length) errors.push(`MISSING_SECTION: ${name}`);
    else if (matches.length > 1) errors.push(`DUPLICATE_SECTION: ${name}`);
    else if (!text(matches[0].content)) errors.push(`EMPTY_SECTION: ${name}`);
  }
  const section = (name: string) => found.find((entry) => entry.name === name)?.content ?? '';

  const claim: Partial<GovernanceClaim> = {};
  const baseline = labeled(section('Baseline'), 'Base SHA');
  if (!baseline || !sha40.test(unquote(baseline))) errors.push('INVALID_BASE_SHA: exact 40-hex base SHA required');
  else claim.baseSha = unquote(baseline);
  if (claim.baseSha && facts.eventBaseSha && claim.baseSha !== facts.eventBaseSha) {
    errors.push(`BASE_SHA_MISMATCH: declared ${claim.baseSha} but the event base is ${facts.eventBaseSha}`);
  }
  if (claim.baseSha && facts.root && facts.headSha) {
    try {
      execFileSync('git', ['-C', facts.root, 'cat-file', '-e', `${claim.baseSha}^{commit}`]);
      execFileSync('git', ['-C', facts.root, 'merge-base', '--is-ancestor', claim.baseSha, facts.headSha]);
    } catch { errors.push(`BASE_NOT_ANCESTOR: ${claim.baseSha} is not available or not an ancestor of ${facts.headSha}`); }
  }

  const ownership = section('Ownership');
  const owner = labeled(ownership, 'DDD owner');
  const phase = labeled(ownership, 'Phase');
  if (!owner || !identifier.test(unquote(owner))) errors.push('INVALID_OWNER: a DDD owner token is required');
  else claim.dddOwner = unquote(owner);
  if (!phase || !unquote(phase)) errors.push('INVALID_PHASE: a phase/work package reference is required');
  else claim.phase = unquote(phase);

  // The declared phase must resolve, the owner must own it, and the declared
  // write set is authorized with the same scope function the host entry uses.
  const collections: PhaseSelection['collection'][] = ['executionSlices', 'workPackages', 'parallelPreparation'];
  let resolvedPhase: ReturnType<typeof resolvePhaseRecord> | undefined;
  if (claim.dddOwner && claim.phase && facts.plan) {
    for (const collection of collections) {
      try { resolvedPhase = resolvePhaseRecord(facts.plan, { collection, id: claim.phase }); break; }
      catch { /* an absent record is the unknown-phase case */ }
    }
    if (!resolvedPhase) errors.push(`UNKNOWN_PHASE: ${claim.phase}`);
    else if (!resolvedPhase.owners.includes(claim.dddOwner)) errors.push(`OWNER_DOES_NOT_OWN_PHASE: ${claim.dddOwner} does not own ${claim.phase}`);
    else {
      const roots = (facts.plan as any)?.sourceRoots;
      if (!roots || !(claim.dddOwner in roots)) errors.push(`INVALID_OWNER: ${claim.dddOwner} is not a development-plan source owner`);
    }
  }

  const writeBullets = bullets(section('Write Set')).map((line) => unquote(line.replace(/^[-*]\s+/u, '')));
  const writeSet: string[] = [];
  for (const entry of writeBullets) {
    if (!relativePath(entry)) errors.push(`INVALID_WRITE_PATH: ${entry || '<empty>'}`);
    else writeSet.push(entry);
  }
  if (!writeSet.length) errors.push('MISSING_WRITE_SET: at least one declared path is required');
  claim.writeSet = writeSet;
  for (const path of facts.changedPaths ?? []) {
    if (!writeSet.some((declaration) => covers(declaration, path))) errors.push(`UNDECLARED_CHANGED_PATH: ${path}`);
  }
  if (resolvedPhase && writeSet.length && facts.root) {
    try { authorizePhaseWriteScope(facts.root, facts.plan, { collection: resolvedPhase.record.workPackage ? 'executionSlices' : 'workPackages', id: claim.phase! }, claim.dddOwner!, writeSet); }
    catch (error: any) { errors.push(`PHASE_SCOPE: ${error?.message ?? error}`); }
  }

  const receipts: ParsedReceipt[] = [];
  let receiptLines = 0;
  for (const line of bullets(section('Receipt Pipeline'))) {
    if (!/^[-*]\s*receipt\s*:/iu.test(line)) continue;
    receiptLines++;
    const entryErrors = errors.length;
    const parts = line.replace(/^[-*]\s*/u, '').split(';').map((part) => part.trim()).filter(Boolean);
    const [head, ...fields] = parts;
    const typeMatch = /^receipt\s*:\s*(.+)$/iu.exec(head);
    const type = typeMatch ? unquote(typeMatch[1]) as ReceiptType : undefined;
    if (!type || !receiptTypes.includes(type)) { errors.push(`UNKNOWN_RECEIPT_TYPE: ${typeMatch?.[1] ?? head}`); continue; }
    const allowed = new Set(['receipt', 'result', ...receiptFields[type]]);
    const parsed: Record<string, string> = {};
    for (const part of fields) {
      const separator = part.indexOf(':');
      const key = (separator === -1 ? part : part.slice(0, separator)).trim().toLowerCase();
      const value = separator === -1 ? '' : unquote(part.slice(separator + 1).trim());
      if (!allowed.has(key)) { errors.push(`UNKNOWN_RECEIPT_FIELD: ${type}: ${key}`); continue; }
      parsed[key] = value;
    }
    for (const required of ['result', ...receiptFields[type].filter((field) => field !== 'proof')]) {
      if (!parsed[required]) errors.push(`MISSING_RECEIPT_FIELD: ${type}: ${required}`);
    }
    if (parsed.result && !receiptResults.includes(parsed.result as ReceiptResult)) {
      errors.push(`INVALID_RECEIPT_FIELD: ${type}: result=${parsed.result}`);
    }
    if (type === 'development-stage') {
      if (parsed.run && !identifier.test(parsed.run)) errors.push(`INVALID_RECEIPT_FIELD: ${type}: run=${parsed.run}`);
      if (parsed.gate && !identifier.test(parsed.gate)) errors.push(`INVALID_RECEIPT_FIELD: ${type}: gate=${parsed.gate}`);
      if (parsed.attempt && !/^[1-9][0-9]*$/u.test(parsed.attempt)) errors.push(`INVALID_RECEIPT_FIELD: ${type}: attempt=${parsed.attempt}`);
      if (parsed.sha256 && !sha256hex.test(parsed.sha256)) errors.push(`INVALID_RECEIPT_FIELD: ${type}: sha256=${parsed.sha256}`);
      if (parsed.run && parsed.gate && parsed.attempt && parsed.path !== receiptPathFor(parsed.run, parsed.gate, parsed.attempt)) {
        errors.push(`RECEIPT_PATH_MISMATCH: ${parsed.path} does not match ${receiptPathFor(parsed.run, parsed.gate, parsed.attempt)}`);
      }
      if (parsed.proof && !proofPattern.test(parsed.proof)) errors.push(`RECEIPT_PATH_INVALID: ${parsed.proof} is not a docs/evidence/development-stage proof`);
    }
    if (type === 'source-check' && parsed.path && !sourceCheckReceiptPattern.test(parsed.path)) {
      errors.push(`RECEIPT_PATH_INVALID: ${parsed.path} is not a docs/evidence/source-checks receipt`);
    }
    const complete = errors.length === entryErrors;
    const values = { ...parsed }; delete values.result;
    receipts.push({ type, result: parsed.result as ReceiptResult, complete, fields: values });
  }
  claim.receipts = receipts.map(({ complete, ...receipt }) => receipt);
  if (!receiptLines) errors.push('RECEIPT_REQUIRED: at least one receipt entry is required');

  const terminal = labeled(section('Terminal State'), 'Terminal state');
  if (!terminal || !['merge-ready', 'source-complete', 'blocked'].includes(unquote(terminal))) {
    errors.push(`INVALID_TERMINAL_STATE: ${terminal ?? '<missing>'}`);
  } else claim.terminalState = unquote(terminal) as TerminalState;
  const mergeDanger = labeled(section('Merge Danger'), 'Merge danger');
  if (!mergeDanger) errors.push('MISSING_MERGE_DANGER: an explicit merge danger statement is required');
  else claim.mergeDanger = mergeDanger;

  const criteria = lines(section('Acceptance Criteria')).filter((line) => /^[-*]\s*\[[ xX]\]/u.test(line));
  if (!criteria.length) errors.push('ACCEPTANCE_CRITERIA_REQUIRED: at least one checklist item is required');
  if (!bullets(section('Verification')).length) errors.push('VERIFICATION_REQUIRED: at least one executed check is required');
  for (const name of ['Business SSOT', 'Development SSOT'] as const) {
    const content = section(name);
    if (!/(?:no (?:business|development) SSOT change|no change|unchanged|not changed)/iu.test(withoutComments(content)) &&
      !/`[A-Za-z0-9._/-]+\.(?:md|json|ts|tsx|py)`/u.test(content)) {
      errors.push(`SSOT_NOT_DECLARED: ${name} must name its canonical owner or state that it is unchanged`);
    }
  }

  const terminalState = claim.terminalState;
  const declared = receipts.filter((receipt) => receipt.complete);
  if (terminalState === 'merge-ready' || terminalState === 'source-complete') {
    for (const receipt of declared) {
      if (receipt.result !== 'passed') errors.push(`OPEN_RECEIPT_RESULT: ${receipt.type}: ${receipt.result}`);
    }
    if (!declared.some((receipt) => receipt.type === 'source-check' || receipt.type === 'development-stage')) {
      errors.push('SOURCE_LAYER_EVIDENCE_REQUIRED: a source check or host development-stage receipt is required');
    }
    if (criteria.some((line) => /^[-*]\s*\[ \]/u.test(line))) errors.push('ACCEPTANCE_NOT_CLOSED: unchecked acceptance criteria remain');
  }

  // Source-check receipts are verified against the checked revision, not restated.
  const changedPaths = facts.changedPaths ?? [];
  for (const receipt of declared.filter((entry) => entry.type === 'source-check')) {
    const path = receipt.fields.path;
    if (!path || !facts.root || !sourceCheckReceiptPattern.test(path)) continue;
    const target = repositoryPath(facts.root, path);
    if (!target || !existsSync(target)) { errors.push(`RECEIPT_NOT_FOUND: ${path}`); continue; }
    let receiptBody: any;
    try { receiptBody = JSON.parse(readFileSync(target, 'utf8')); } catch { errors.push(`RECEIPT_INVALID: ${path}`); continue; }
    if (!receiptBody || typeof receiptBody !== 'object' || Array.isArray(receiptBody)) { errors.push(`RECEIPT_INVALID: ${path}`); continue; }
    if (receiptBody.evidenceLayer !== 'source') errors.push(`RECEIPT_LAYER_MISMATCH: ${path} is not a source-layer receipt`);
    if (receiptBody.result !== 'pass') errors.push(`RECEIPT_NOT_PASSING: ${path} records result ${receiptBody.result}`);
    if (!sha40.test(String(receiptBody.sourceBaseSha ?? ''))) errors.push(`RECEIPT_INVALID: ${path} lacks a source base SHA`);
    else if (claim.baseSha && receiptBody.sourceBaseSha !== claim.baseSha) {
      errors.push(`RECEIPT_BASE_MISMATCH: ${path} binds ${receiptBody.sourceBaseSha} but the body declares ${claim.baseSha}`);
    }
    const receiptWriteSet = Array.isArray(receiptBody.writeSet) ? receiptBody.writeSet : [];
    if (!receiptWriteSet.length || !receiptWriteSet.every((entry: unknown) => typeof entry === 'string' && relativePath(entry))) {
      errors.push(`RECEIPT_INVALID: ${path} has no valid write set`);
    } else {
      for (const entry of receiptWriteSet as string[]) {
        if (entry !== path && !writeSet.some((declaration) => covers(declaration, entry))) {
          errors.push(`RECEIPT_WRITE_SET_MISMATCH: ${path} covers undeclared path ${entry}`);
        }
      }
      for (const changed of changedPaths) {
        if (changed !== path && !(receiptWriteSet as string[]).some((entry) => covers(entry, changed))) {
          errors.push(`RECEIPT_WRITE_SET_MISMATCH: ${path} does not cover changed path ${changed}`);
        }
      }
    }
    // The recorded file hashes must equal the checked content; a receipt whose
    // inputs moved on is stale and cannot certify the revision under review.
    const hashes = receiptBody.verifiedSource?.changedFilesSha256;
    if (!hashes || typeof hashes !== 'object' || Array.isArray(hashes) || !Object.keys(hashes).length) {
      errors.push(`RECEIPT_INVALID: ${path} records no verified file hashes`);
    } else {
      for (const [hashedPath, hashedValue] of Object.entries(hashes as Record<string, unknown>)) {
        if (!relativePath(hashedPath) || !sha256hex.test(String(hashedValue))) { errors.push(`RECEIPT_INVALID: ${path} hash entry ${hashedPath}`); continue; }
        const file = repositoryPath(facts.root, hashedPath);
        if (!file || !existsSync(file)) { errors.push(`RECEIPT_HASH_PATH_MISSING: ${path} records ${hashedPath}`); continue; }
        const actual = digest(readFileSync(file));
        if (actual !== hashedValue) errors.push(`RECEIPT_STALE_HASH: ${path} recorded ${hashedPath} as ${hashedValue} but it is now ${actual}`);
      }
    }
  }

  // Merge authorization: every development-stage declaration is checked against
  // the trusted host store or a signed host proof; the trust root is never
  // taken from the pull request.
  const stageReceipts = declared.filter((entry) => entry.type === 'development-stage');
  if (stageReceipts.length) {
    authorization.claimed = true;
    for (const entry of stageReceipts) {
      const runId = entry.fields.run; const gateId = entry.fields.gate; const attempt = Number(entry.fields.attempt);
      const runErrors: string[] = [];
      try {
        if (!claim.dddOwner || !claim.phase || !claim.baseSha || !facts.root || !facts.headSha || !facts.plan) {
          throw new Error('MERGE_AUTHORIZATION_CONTEXT: owner, phase, base, plan and checked revision are required');
        }
        const proofPath = entry.fields.proof;
        const hostStore = facts.hostStore ? openTrustedStore(facts.root, facts.hostStore) : undefined;
        const authority = decodeAuthority(facts.authorityPublicKey);
        if (!hostStore && !proofPath) throw new Error('MERGE_AUTHORIZATION_UNPROVEN: a trusted host store or signed host proof is required');
        if (!hostStore && !authority) throw new Error('MERGE_AUTHORIZATION_UNCONFIGURED: the trusted host public key is not configured');

        let approval: RunApproval; let receipt: StageReceipt; let receiptHash: string;
        if (hostStore) {
          approval = readRunApproval(hostStore, runId);
          const storeEntry = stageReceiptEvidence(hostStore, runId, gateId, attempt);
          receipt = storeEntry.receipt; receiptHash = storeEntry.hash;
          authorization.basis = 'host-store';
        } else {
          const proofTarget = repositoryPath(facts.root, proofPath!);
          if (!proofTarget || !existsSync(proofTarget)) throw new Error(`EVIDENCE_INVALID: signed proof missing: ${proofPath}`);
          const proof = JSON.parse(readFileSync(proofTarget, 'utf8'));
          if (!proof || typeof proof !== 'object' || Array.isArray(proof) || proof.schemaVersion !== 1 || proof.kind !== 'opl.development.stage.proof.v1') {
            throw new Error('EVIDENCE_INVALID: proof structure');
          }
          const key = createPublicKey(authority!);
          const unwrap = (envelope: any, label: string) => {
            if (!envelope || typeof envelope !== 'object' || typeof envelope.signature !== 'string' || envelope.payload === undefined ||
              !verify(null, Buffer.from(canonical(envelope.payload)), key, Buffer.from(envelope.signature, 'base64'))) {
              throw new Error(`EVIDENCE_INVALID: ${label} signature mismatch`);
            }
            return envelope.payload;
          };
          approval = parseApproval(unwrap(proof.approval, 'approval'));
          receipt = parseStageReceipt(unwrap(proof.receipt, 'receipt'), runId, gateId, attempt);
          receiptHash = digest(canonical(receipt));
          authorization.basis = 'signed-proof';
        }

        if (receipt.result !== 'passed') throw new Error(`MERGE_AUTHORIZATION_PENDING: ${runId}/${gateId} is ${receipt.result}`);
        if (approval.runId !== runId) throw new Error('EVIDENCE_INVALID: run identity mismatch');
        if (approval.selection.collection !== undefined && !approval.gates.some((candidate) => candidate.id === gateId)) {
          throw new Error(`EVIDENCE_INVALID: undeclared gate ${gateId}`);
        }
        if (approval.owner !== claim.dddOwner || approval.selection.id !== claim.phase) {
          throw new Error(`MERGE_AUTHORIZATION_PHASE: the approved run ${runId} is ${approval.owner}/${approval.selection.id}`);
        }
        if (approval.baseSha !== claim.baseSha) throw new Error(`EVIDENCE_BASE_MISMATCH: the approved run binds ${approval.baseSha}`);
        // Changed paths must stay inside the host-approved write scope, resolved
        // by the same phase authorization the worker entry uses.
        const authorized = authorizePhaseWriteScope(facts.root, facts.plan, approval.selection, approval.owner, approval.writePaths);
        const granted = [...authorized.writePaths, ...authorized.testWritePaths];
        for (const changed of facts.changedPaths ?? []) {
          if (!granted.some((declaration) => (declaration.endsWith('/') ? changed.startsWith(declaration) : changed === declaration))) {
            throw new Error(`MERGE_AUTHORIZATION_SCOPE: ${changed} is outside the approved write scope of ${runId}`);
          }
        }
        if (receiptHash !== entry.fields.sha256) throw new Error(`EVIDENCE_INVALID: declared receipt digest ${entry.fields.sha256} != ${receiptHash}`);
        if (receipt.approvalHash !== digest(canonical(approval))) throw new Error('EVIDENCE_INVALID: receipt does not bind the approved run');
        if (receipt.phaseHash !== resolveApprovedPhase(facts.root, approval).fingerprint) throw new Error('EVIDENCE_STALE_PHASE: the approved phase record changed');
        if (receipt.runnerHash !== currentRunnerHash()) throw new Error('EVIDENCE_INVALID: acceptance runner changed since the receipt');
        const gate = approval.gates.find((candidate) => candidate.id === gateId)!;
        if (receipt.inputHash !== gateInputHash(facts.root, gate)) throw new Error('EVIDENCE_STALE_INPUT: declared stage inputs changed since the receipt');
        for (const dependency of receipt.dependencies) {
          const bound = hostStore
            ? receiptEvidenceByHash(hostStore, dependency.runId, dependency.gateId, dependency.receiptHash)
            : undefined;
          if (!bound) throw new Error(`EVIDENCE_INVALID: dependency proof missing: ${dependency.runId}/${dependency.gateId}`);
        }
        const sourceSha = receipt.sourceSha;
        execFileSync('git', ['-C', facts.root, 'cat-file', '-e', `${sourceSha}^{commit}`]);
        execFileSync('git', ['-C', facts.root, 'merge-base', '--is-ancestor', sourceSha, facts.headSha]);
        const successors = execFileSync('git', ['-C', facts.root, 'diff', '--name-only', '-z', '--no-renames', `${sourceSha}..${facts.headSha}`, '--'], { encoding: 'utf8' }).split('\0').filter(Boolean);
        for (const successor of successors) {
          if (successor !== proofPath) throw new Error(`MERGE_AUTHORIZATION_UNPROVEN: post-receipt change ${successor} is not proof-only`);
        }
        authorization.ok = true; authorization.runId = runId; authorization.gateId = gateId; authorization.attempt = attempt;
        authorization.receiptDigest = receiptHash;
      } catch (error: any) {
        runErrors.push(String(error?.message ?? error));
      }
      authorization.errors.push(...runErrors);
    }
  }

  if (terminalState === 'merge-ready') {
    if (!stageReceipts.length) errors.push('MERGE_AUTHORIZATION_UNPROVEN: merge-ready requires a host development-stage receipt');
    else if (!authorization.ok) errors.push(...authorization.errors.map((message) => `MERGE_AUTHORIZATION: ${message}`));
  } else if (declared.some((entry) => entry.type === 'development-stage') && !authorization.ok) {
    errors.push(...authorization.errors.map((message) => `MERGE_AUTHORIZATION: ${message}`));
  }
  if (mode === 'merge' && terminalState !== 'merge-ready') {
    errors.push('MERGE_AUTHORIZATION_NOT_CLAIMED: a merge gate requires a verified merge-ready claim');
  }

  const ok = errors.length === 0;
  return { ok, errors, authorization, ...(ok ? { claim: claim as GovernanceClaim } : {}) };
}

/** The pull-request change scope: the base diff plus the local index and worktree. */
function repositoryChangeScope(root: string, base: string, head: string): string[] {
  const names = (...args: string[]) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8' }).split('\0').filter(Boolean);
  return [...new Set([
    ...names('diff', '--name-only', '-z', '--no-renames', `${base}...${head}`, '--'),
    // A local run has not committed yet; the staged, unstaged and untracked
    // paths are part of the same claim the pull request will publish. In CI
    // the checkout is clean and these sets are empty.
    ...names('diff', '--cached', '--name-only', '-z', '--no-renames', base, '--'),
    ...names('diff', '--name-only', '-z', '--no-renames', base, '--'),
    ...names('ls-files', '--others', '--exclude-standard', '-z'),
  ])];
}

function usage(): never {
  throw new Error('usage: check-pr-governance.ts (--body-file <path> | --body-env <NAME>) [--plan <path>] [--root <path>] [--base <40-hex-sha> | --base-env <NAME>] [--head <sha> | --head-env <NAME>] [--mode record|merge] [--host-store <absolute path>] [--authority-env <NAME>] [--bootstrap-base <existing base worktree directory>]');
}
/**
 * Bootstrap downgrade is a physical condition, never an actor exemption: when
 * the checked pull request's base revision has no checker file, merge mode
 * enforces record compliance only. A base that carries the checker keeps full
 * merge enforcement.
 */
export function effectiveGovernanceMode(mode: GovernanceMode, bootstrapBase: string | undefined): { mode: GovernanceMode; bootstrap: boolean } {
  if (mode !== 'merge' || bootstrapBase === undefined) return { mode, bootstrap: false };
  const base = resolve(bootstrapBase);
  if (!existsSync(base) || !statSync(base).isDirectory()) throw new Error(`invalid bootstrap base: ${bootstrapBase}`);
  const hasChecker = existsSync(resolve(base, 'tools/check-pr-governance.ts'));
  return hasChecker ? { mode: 'merge', bootstrap: false } : { mode: 'record', bootstrap: true };
}
export function governanceCli(argv: string[], environment: NodeJS.ProcessEnv = process.env) {
  const options = new Map<string, string>();
  for (let index = 0; index < argv.length; index += 2) {
    const flag = argv[index];
    const value = argv[index + 1];
    if (!flag?.startsWith('--') || value === undefined) usage();
    options.set(flag, value);
  }
  for (const flag of options.keys()) if (!['--body-file', '--body-env', '--plan', '--root', '--base', '--base-env', '--head', '--head-env', '--mode', '--host-store', '--authority-env', '--bootstrap-base'].includes(flag)) usage();
  const bodyFile = options.get('--body-file');
  const bodyEnv = options.get('--body-env');
  if (Boolean(bodyFile) === Boolean(bodyEnv)) usage();
  const body = bodyFile ? readFileSync(resolve(bodyFile), 'utf8') : environment[bodyEnv!] ?? '';
  const root = realpathSync(resolve(options.get('--root') ?? execFileSync('git', ['rev-parse', '--show-toplevel'], { encoding: 'utf8' }).trim()));
  const planPath = options.get('--plan') ?? 'docs/spec/target/checks/development_plan.json';
  const plan = JSON.parse(readFileSync(resolve(root, planPath), 'utf8'));
  let base = options.get('--base') ?? (options.get('--base-env') ? environment[options.get('--base-env')!] : undefined);
  const head = options.get('--head') ?? (options.get('--head-env') ? environment[options.get('--head-env')!] : undefined) ??
    execFileSync('git', ['-C', root, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  if (base && !sha40.test(base)) {
    // A ref is resolved to the exact commit; an abbreviated SHA literal is
    // refused so the recorded base is always complete.
    if (/^[0-9a-f]{1,39}$/iu.test(base)) throw new Error(`abbreviated base SHA refused: ${base}`);
    const resolved = execFileSync('git', ['-C', root, 'rev-parse', '--verify', `${base}^{commit}`], { encoding: 'utf8' }).trim();
    if (!sha40.test(resolved)) throw new Error(`invalid base SHA: ${base}`);
    base = resolved;
  }
  const requestedMode = (options.get('--mode') ?? 'record') as GovernanceMode;
  if (!['record', 'merge'].includes(requestedMode)) usage();
  const { mode, bootstrap } = effectiveGovernanceMode(requestedMode, options.get('--bootstrap-base'));
  const authorityEnv = options.get('--authority-env');
  const changedPaths = base ? repositoryChangeScope(root, base, head) : undefined;
  const result = checkPullRequestBody(body, {
    plan, root, changedPaths, eventBaseSha: base, headSha: head, mode,
    hostStore: options.get('--host-store'),
    authorityPublicKey: authorityEnv ? environment[authorityEnv] : undefined,
  });
  process.stdout.write(JSON.stringify({ ok: result.ok, mode: requestedMode, effectiveMode: mode, bootstrap, errors: result.errors, claim: result.claim, authorization: result.authorization, changedPaths, baseSha: base, headSha: head }, null, 2) + '\n');
  if (!result.ok) { process.stderr.write('PR_GOVERNANCE_FAILED\n'); process.exitCode = 1; }
  return result;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { governanceCli(process.argv.slice(2)); } catch (error: any) { console.error(error.message); process.exitCode = 1; }
}

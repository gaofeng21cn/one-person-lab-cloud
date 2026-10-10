// Focused gate for the Step 4 host-side wiring: the framework slice manifest
// (reference-only) is converted into the existing Cloud host admission record,
// and a framework receipt reference is resolved against an existing host store.
// Zero dependencies: node built-ins only. All state lives in temporary
// directories; nothing here touches an evidence store.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

import { consumeReceiptReference, convertManifest, resolveRequires } from '../../tools/dev-frame-bridge.ts';

const BASE_SHA = '34878da668b0c66f7d3cd4c79967cd94907f1cf3';
const HASH_A = 'a'.repeat(64);
const HASH_B = 'b'.repeat(64);

// Mirror of the host store's content addressing, used only to build fixtures
// whose hashes the store reader will re-derive.
const canonical = (value: any): string => {
  if (Array.isArray(value)) return '[' + value.map(canonical).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.keys(value).sort().map((key) => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}';
  return JSON.stringify(value);
};
const sha256 = (text: string) => createHash('sha256').update(text).digest('hex');
const receiptAddress = (receipt: Record<string, unknown>) => sha256(canonical(receipt));

function tempRoot(prefix: string): string {
  return mkdtempSync(join(tmpdir(), prefix));
}
function writeJson(path: string, value: unknown) {
  writeFileSync(path, JSON.stringify(value, null, 2) + '\n');
}

/** Minimal stand-in for the framework validators' exported contract. */
function writeStubFramework(root: string) {
  const lib = join(root, 'validators', 'lib');
  mkdirSync(lib, { recursive: true });
  writeFileSync(join(lib, 'manifest.mjs'), `
const ALLOWED = ['sliceId', 'selection', 'owner', 'baseline', 'writeSet', 'readPaths', 'gates', 'requires', 'references'];
const SECOND_SSOT = ['status', 'progress', 'currentState', 'ownerAllocation'];
const HOST_OWNED = ['result', 'verification', 'receiptHash', 'approvalHash', 'phaseHash', 'inputHash', 'runnerHash', 'dependencies'];
export function validateSliceManifest(record) {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return { valid: false, code: 'SCHEMA_INVALID', detail: 'manifest must be an object' };
  for (const key of Object.keys(record)) {
    if (ALLOWED.includes(key)) continue;
    if (SECOND_SSOT.includes(key)) return { valid: false, code: 'SECOND_SSOT_FIELD', detail: key };
    if (HOST_OWNED.includes(key)) return { valid: false, code: 'HOST_OWNED_FIELD', detail: key };
    return { valid: false, code: 'SCHEMA_INVALID', detail: 'unknown field ' + key };
  }
  for (const key of ALLOWED) if (!(key in record)) return { valid: false, code: 'SCHEMA_INVALID', detail: 'missing ' + key };
  if (!/^[a-f0-9]{40}$/.test((record.baseline && record.baseline.sha) || '')) return { valid: false, code: 'SCHEMA_INVALID', detail: 'baseline.sha' };
  if (!Array.isArray(record.writeSet) || !record.writeSet.length) return { valid: false, code: 'SCHEMA_INVALID', detail: 'writeSet' };
  return { valid: true };
}
`);
  writeFileSync(join(lib, 'stage-receipt.mjs'), `
export function validateStageReceipt(record) {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return { valid: false, code: 'SCHEMA_INVALID', detail: 'receipt must be an object' };
  if (record.schemaVersion !== 1 || record.kind !== 'opl.development.stage.v1') return { valid: false, code: 'SCHEMA_INVALID', detail: 'identity' };
  if (!['passed', 'failed', 'blocked'].includes(record.result)) return { valid: false, code: 'SCHEMA_INVALID', detail: 'result' };
  const v = record.verification || {};
  if (record.result === 'passed' && !(v.exitCode === 0 && v.tests > 0 && v.failed === 0 && v.skipped === 0)) {
    return { valid: false, code: 'PASS_WITHOUT_EXECUTION', detail: 'passed requires executed evidence' };
  }
  return { valid: true };
}
`);
  writeFileSync(join(lib, 'receipt-reference.mjs'), `
export function validateReceiptReference(record, options = {}) {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return { valid: false, code: 'SCHEMA_INVALID', detail: 'reference must be an object' };
  const v = record.verification || {};
  if (record.result === 'passed' && !(v.exitCode === 0 && v.tests > 0 && v.failed === 0 && v.skipped === 0 && v.todo === 0)) {
    return { valid: false, code: 'PASS_WITHOUT_EXECUTION', detail: 'passed requires executed evidence' };
  }
  const receipts = options.receipts;
  if (receipts && typeof receipts.get === 'function') {
    const upstream = receipts.get(record.receiptHash);
    if (!upstream) return { valid: false, code: 'UNRESOLVED_RECEIPT', detail: record.receiptHash };
    if (upstream.result !== 'passed') return { valid: false, code: 'UPSTREAM_NOT_PASSED', detail: upstream.result };
  }
  return { valid: true };
}
`);
}

function manifestFixture(extra: Record<string, unknown> = {}) {
  return {
    sliceId: 'step4-selection-decoder',
    selection: { collection: 'executionSlices', id: 'W01.application-contracts' },
    owner: 'cloud',
    baseline: { repo: 'gaofeng21cn/one-person-lab-cloud', sha: BASE_SHA },
    writeSet: ['packages/contracts/go/workspace_application_selection.go', 'packages/contracts/go/workspace_application_selection_test.go'],
    readPaths: ['packages/contracts/go'],
    gates: ['contracts-decoder'],
    requires: [],
    references: [{ repo: 'RenDeHuang/opl-development-framework', path: 'validators/lib/manifest.mjs', sha256: HASH_B }],
    ...extra,
  };
}

function gatesFixture() {
  return [{ id: 'contracts-decoder', kind: 'go', inputs: ['packages/contracts/go'], cwd: 'packages/contracts/go', needs: [] }];
}

function approvalFixture(runId: string, selectionId: string) {
  return {
    schemaVersion: 1,
    runId,
    baseSha: BASE_SHA,
    planPath: 'docs/spec/target/checks/development_plan.json',
    selection: { collection: 'executionSlices', id: selectionId },
    owner: 'cloud',
    readPaths: ['packages/contracts/go'],
    writePaths: ['packages/contracts/go'],
    gates: [{ id: 'contracts-decoder', kind: 'go', inputs: ['packages/contracts/go'], cwd: 'packages/contracts/go', needs: [] }],
    requires: {},
  };
}

function stageReceiptFixture(overrides: Record<string, unknown> = {}) {
  return {
    schemaVersion: 1,
    kind: 'opl.development.stage.v1',
    runId: 'step4-selection-decoder',
    gateId: 'contracts-decoder',
    attempt: 1,
    evidenceLayer: 'source',
    sourceSha: BASE_SHA,
    approvalHash: HASH_A,
    phaseHash: HASH_A,
    runnerHash: HASH_A,
    inputHash: HASH_A,
    dependencies: [],
    result: 'passed',
    checkedAt: '2026-10-10T08:00:00.000Z',
    verification: { tests: 3, failed: 0, skipped: 0, exitCode: 0, outputSha256: HASH_A },
    ...overrides,
  };
}

/** A store with one upstream run: approval + one stage receipt. */
function upstreamStore(root: string, receipt: Record<string, unknown>) {
  const runDir = join(root, 'runs', 'step4-selection-decoder');
  mkdirSync(join(runDir, 'receipts'), { recursive: true });
  writeJson(join(runDir, 'approval.json'), approvalFixture('step4-selection-decoder', 'W01.application-contracts'));
  writeJson(join(runDir, 'receipts', 'contracts-decoder-1.json'), receipt);
}

test('converts a valid framework manifest into the existing host admission record', async () => {
  const root = tempRoot('frame-convert-');
  try {
    writeStubFramework(root);
    const manifestPath = join(root, 'manifest.json');
    const gatesPath = join(root, 'gates.json');
    writeJson(manifestPath, manifestFixture());
    writeJson(gatesPath, gatesFixture());
    const approval = await convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' });
    assert.equal(approval.schemaVersion, 1);
    assert.equal(approval.runId, 'step4-selection-decoder');
    assert.equal(approval.baseSha, BASE_SHA);
    assert.equal(approval.owner, 'cloud');
    assert.deepEqual(approval.selection, { collection: 'executionSlices', id: 'W01.application-contracts' });
    assert.deepEqual(approval.writePaths, ['packages/contracts/go/workspace_application_selection.go', 'packages/contracts/go/workspace_application_selection_test.go']);
    assert.deepEqual(approval.readPaths, ['packages/contracts/go']);
    assert.deepEqual(approval.requires, {});
    assert.equal(approval.gates.length, 1);
    assert.equal(approval.gates[0].id, 'contracts-decoder');
    assert.equal(approval.gates[0].kind, 'go');
    assert.equal(approval.gates[0].cwd, 'packages/contracts/go');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('refuses a manifest that carries a second-SSOT field', async () => {
  const root = tempRoot('frame-second-ssot-');
  try {
    writeStubFramework(root);
    const manifestPath = join(root, 'manifest.json');
    const gatesPath = join(root, 'gates.json');
    writeJson(manifestPath, manifestFixture({ progress: { done: 1 } }));
    writeJson(gatesPath, gatesFixture());
    await assert.rejects(
      () => convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' }),
      /SECOND_SSOT_FIELD/u,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('refuses a manifest that carries a host-owned execution field', async () => {
  const root = tempRoot('frame-host-owned-');
  try {
    writeStubFramework(root);
    const manifestPath = join(root, 'manifest.json');
    const gatesPath = join(root, 'gates.json');
    writeJson(manifestPath, manifestFixture({ result: 'passed' }));
    writeJson(gatesPath, gatesFixture());
    await assert.rejects(
      () => convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' }),
      /HOST_OWNED_FIELD/u,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('refuses gate definitions that do not match the manifest gate ids', async () => {
  const root = tempRoot('frame-gate-coverage-');
  try {
    writeStubFramework(root);
    const manifestPath = join(root, 'manifest.json');
    const gatesPath = join(root, 'gates.json');
    writeJson(manifestPath, manifestFixture());
    writeJson(gatesPath, []);
    await assert.rejects(
      () => convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' }),
      /gate/u,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('carries the host-owned isolated database declaration and refuses misuse', async () => {
  const root = tempRoot('frame-database-gate-');
  try {
    writeStubFramework(root);
    const manifestPath = join(root, 'manifest.json');
    const gatesPath = join(root, 'gates.json');
    writeJson(manifestPath, manifestFixture());
    writeJson(gatesPath, [{ ...gatesFixture()[0], database: 'isolated-owner-postgres' }]);
    const approval = await convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' });
    assert.equal(approval.gates[0].database, 'isolated-owner-postgres');
    writeJson(gatesPath, [{ ...gatesFixture()[0], database: 'production-postgres' }]);
    await assert.rejects(
      () => convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' }),
      /unknown isolated database fixture/u,
    );
    writeJson(gatesPath, [{ id: 'contracts-decoder', kind: 'node', inputs: ['packages/contracts/go'], targets: ['tests/tools/dev-frame-bridge.test.ts'], needs: [], database: 'isolated-owner-postgres' }]);
    await assert.rejects(
      () => convertManifest({ manifestPath, frameworkRoot: root, gatesPath, store: join(root, 'store'), planPath: 'docs/spec/target/checks/development_plan.json' }),
      /only applies to Go checks/u,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('an unrelated malformed receipt never affects the named gate consumption', async () => {
  const root = tempRoot('frame-unrelated-receipt-');
  try {
    writeStubFramework(root);
    const receipt = stageReceiptFixture();
    const store = join(root, 'store');
    upstreamStore(store, receipt);
    const receipts = join(store, 'runs', 'step4-selection-decoder', 'receipts');
    // A different gate's corrupt record must not be opened while the named
    // gate is consumed: receipt lookup reads only the named attempt sequence.
    writeFileSync(join(receipts, 'legacy-gate-1.json'), '{ this is not json');
    const manifest = manifestFixture({ requires: [{ runId: 'step4-selection-decoder', gateId: 'contracts-decoder', receiptHash: receiptAddress(receipt) }] });
    const requires = resolveRequires(manifest as any, store);
    assert.deepEqual(requires, { 'W01.application-contracts': { runId: 'step4-selection-decoder', gateId: 'contracts-decoder' } });
    const validators = await (await import('../../tools/dev-frame-bridge.ts')).loadFrameValidators(root);
    const reference = {
      runId: 'step4-selection-decoder',
      gateId: 'contracts-decoder',
      attempt: 1,
      receiptHash: receiptAddress(receipt),
      result: 'passed',
      verification: { tests: 3, failed: 0, skipped: 0, todo: 0, exitCode: 0, outputSha256: HASH_A },
    };
    assert.equal(consumeReceiptReference(validators, store, reference).receiptHash, receiptAddress(receipt));
    // A corrupt record of the requested gate itself still fails closed.
    writeFileSync(join(receipts, 'contracts-decoder-2.json'), '{ not json');
    assert.throws(() => resolveRequires(manifest as any, store), SyntaxError);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('resolves a downstream requires binding by the upstream record and its receipt hash', () => {
  const root = tempRoot('frame-requires-');
  try {
    const receipt = stageReceiptFixture();
    upstreamStore(root, receipt);
    const manifest = manifestFixture({ requires: [{ runId: 'step4-selection-decoder', gateId: 'contracts-decoder', receiptHash: receiptAddress(receipt) }] });
    const requires = resolveRequires(manifest as any, root);
    assert.deepEqual(requires, { 'W01.application-contracts': { runId: 'step4-selection-decoder', gateId: 'contracts-decoder' } });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('refuses a requires binding whose declared receipt hash does not match the stored receipt', () => {
  const root = tempRoot('frame-requires-mismatch-');
  try {
    const receipt = stageReceiptFixture();
    upstreamStore(root, receipt);
    const manifest = manifestFixture({ requires: [{ runId: 'step4-selection-decoder', gateId: 'contracts-decoder', receiptHash: HASH_B }] });
    assert.throws(() => resolveRequires(manifest as any, root), /hash/u);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('consumes a receipt reference by hash and returns the stored receipt', async () => {
  const root = tempRoot('frame-consume-');
  try {
    writeStubFramework(root);
    const receipt = stageReceiptFixture();
    const store = join(root, 'store');
    upstreamStore(store, receipt);
    const validators = await (await import('../../tools/dev-frame-bridge.ts')).loadFrameValidators(root);
    const reference = {
      runId: 'step4-selection-decoder',
      gateId: 'contracts-decoder',
      attempt: 1,
      receiptHash: receiptAddress(receipt),
      result: 'passed',
      verification: { tests: 3, failed: 0, skipped: 0, todo: 0, exitCode: 0, outputSha256: HASH_A },
    };
    const resolved = consumeReceiptReference(validators, store, reference);
    assert.equal(resolved.receiptHash, receiptAddress(receipt));
    assert.equal((resolved.receipt as any).gateId, 'contracts-decoder');
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('refuses a receipt reference that resolves to a non-passed upstream receipt', async () => {
  const root = tempRoot('frame-consume-failed-');
  try {
    writeStubFramework(root);
    const receipt = stageReceiptFixture({ result: 'failed', verification: { tests: 3, failed: 1, skipped: 0, exitCode: 1, outputSha256: HASH_A } });
    const store = join(root, 'store');
    upstreamStore(store, receipt);
    const validators = await (await import('../../tools/dev-frame-bridge.ts')).loadFrameValidators(root);
    const reference = {
      runId: 'step4-selection-decoder',
      gateId: 'contracts-decoder',
      attempt: 1,
      receiptHash: receiptAddress(receipt),
      result: 'passed',
      verification: { tests: 3, failed: 0, skipped: 0, todo: 0, exitCode: 0, outputSha256: HASH_A },
    };
    assert.throws(() => consumeReceiptReference(validators, store, reference), /UPSTREAM_NOT_PASSED/u);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('refuses a receipt reference whose hash is absent from the store', async () => {
  const root = tempRoot('frame-consume-absent-');
  try {
    writeStubFramework(root);
    const receipt = stageReceiptFixture();
    const store = join(root, 'store');
    upstreamStore(store, receipt);
    const validators = await (await import('../../tools/dev-frame-bridge.ts')).loadFrameValidators(root);
    const reference = {
      runId: 'step4-selection-decoder',
      gateId: 'contracts-decoder',
      attempt: 1,
      receiptHash: HASH_B,
      result: 'passed',
      verification: { tests: 3, failed: 0, skipped: 0, todo: 0, exitCode: 0, outputSha256: HASH_A },
    };
    assert.throws(() => consumeReceiptReference(validators, store, reference), /EVIDENCE_INVALID|hash/u);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { createHash, createPrivateKey, generateKeyPairSync, sign } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
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
const receiptDigest = (payload: unknown) => sha256(canonical(payload));
function put(root: string, path: string, content: string) {
  mkdirSync(resolve(root, path, '..'), { recursive: true });
  writeFileSync(join(root, path), content);
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
    selection: { collection: 'workPackages', id: 'W01' }, owner: 'owner', readPaths: ['owner/'], writePaths: ['owner/input.ts', 'owner/new.ts', 'owner/acceptance.test.mjs'],
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

test('a real signed host receipt authorizes merge-ready and forged, foreign or stale claims are refused', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// admitted implementation change\n');
  const verified = await session.verifyGate('acceptance') as any;
  assert.equal(verified.result, 'passed', JSON.stringify(verified));
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const envelopePath = join(store, receiptPath);
  const envelope = JSON.parse(readFileSync(envelopePath, 'utf8'));
  const digest = receiptDigest(envelope.payload);
  const stageEntry = `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\``;
  const base = approval.baseSha;
  const head = git('rev-parse', 'HEAD').trim();
  const facts = { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: head, mode: 'merge', hostStore: store } as const;

  const authorized = checkPullRequestBody(render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': stageEntry }), facts);
  assert.equal(authorized.ok, true, authorized.errors.join('\n'));
  assert.equal(authorized.authorization?.basis, 'host-store');
  assert.equal(authorized.authorization?.ok, true);

  // A run without a signed approval in the trusted store.
  const ghost = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`runs/ghost-run/receipts/acceptance-1.json\`; run: \`ghost-run\`; gate: \`acceptance\`; attempt: 1; sha256: \`${'0'.repeat(64)}\`` }),
    facts,
  );
  assert.equal(ghost.ok, false);
  assert.match(ghost.errors.join('\n'), /EVIDENCE_INVALID/);

  // The signed receipt of another run is not this run's evidence.
  development.approveRun(root, store, { ...approval, runId: 'second-run' });
  mkdirSync(join(store, 'runs', 'second-run', 'receipts'), { recursive: true });
  writeFileSync(join(store, 'runs', 'second-run', 'receipts', 'acceptance-1.json'), readFileSync(envelopePath));
  const foreign = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`runs/second-run/receipts/acceptance-1.json\`; run: \`second-run\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\`` }),
    facts,
  );
  assert.equal(foreign.ok, false);
  assert.match(foreign.errors.join('\n'), /EVIDENCE_INVALID/);

  // A self-made key cannot sign for the trusted store.
  const rogue = generateKeyPairSync('ed25519');
  const rogueEnvelope = { payload: { ...envelope.payload }, signature: sign(null, Buffer.from(canonical(envelope.payload)), rogue.privateKey).toString('base64') };
  writeFileSync(envelopePath, JSON.stringify(rogueEnvelope));
  const selfSigned = checkPullRequestBody(render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': stageEntry }), facts);
  assert.equal(selfSigned.ok, false);
  assert.match(selfSigned.errors.join('\n'), /EVIDENCE_INVALID/);
  writeFileSync(envelopePath, JSON.stringify(envelope));

  // A payload change without the host signature is not evidence.
  const tampered = { ...envelope, payload: { ...envelope.payload, verification: { ...envelope.payload.verification, outputSha256: sha256('tampered') } } };
  writeFileSync(envelopePath, JSON.stringify(tampered));
  const unsigned = checkPullRequestBody(render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': stageEntry }), facts);
  assert.equal(unsigned.ok, false);
  assert.match(unsigned.errors.join('\n'), /EVIDENCE_INVALID/);
  writeFileSync(envelopePath, JSON.stringify(envelope));

  // The receipt stays signed but no longer matches the checked revision's inputs.
  writeFileSync(join(root, 'owner/input.ts'), 'export const answer = 43;\n');
  const stale = checkPullRequestBody(render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': stageEntry }), facts);
  assert.equal(stale.ok, false);
  assert.match(stale.errors.join('\n'), /EVIDENCE_STALE_INPUT/);
});

test('the signed approval binds the declared base and clone directories cannot supply a trusted store', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context(); await session.verifyGate('acceptance');
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const envelope = JSON.parse(readFileSync(join(store, receiptPath), 'utf8'));
  const stageEntry = `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${receiptDigest(envelope.payload)}\``;

  // A trusted store located inside the repository would be author-controlled data.
  const inside = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${approval.baseSha}\``, 'Receipt Pipeline': stageEntry }),
    { plan, root, changedPaths: [], eventBaseSha: approval.baseSha, headSha: git('rev-parse', 'HEAD').trim(), mode: 'merge', hostStore: join(root, 'docs') },
  );
  assert.equal(inside.ok, false);
  assert.match(inside.errors.join('\n'), /HOST_STORE_INSIDE_REPOSITORY|TRUSTED_STORE/);

  // The host approval binds one base; a body declaring another base is refused.
  const approvalPath = join(store, 'runs', approval.runId, 'approval.json');
  const signedApproval = JSON.parse(readFileSync(approvalPath, 'utf8'));
  signedApproval.payload.baseSha = 'c'.repeat(40);
  const key = createPrivateKey(readFileSync(join(store, 'authority.key')));
  signedApproval.signature = sign(null, Buffer.from(canonical(signedApproval.payload)), key).toString('base64');
  writeFileSync(approvalPath, JSON.stringify(signedApproval));
  const mismatch = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${approval.baseSha}\``, 'Receipt Pipeline': stageEntry }),
    { plan, root, changedPaths: [], eventBaseSha: approval.baseSha, headSha: git('rev-parse', 'HEAD').trim(), mode: 'merge', hostStore: store },
  );
  assert.equal(mismatch.ok, false);
  assert.match(mismatch.errors.join('\n'), /EVIDENCE_BASE_MISMATCH/);
});

test('a signed host proof exported after the candidate commit authorizes merge in CI without the host store', async t => {
  const { root, store, git, plan, approval: base } = fixture(t);
  const proofPath = 'docs/evidence/development-stage/owner-run-acceptance-1.json';
  // The host approval declares the proof export explicitly; the phase record's
  // evidence prefixes authorize it without granting any owner root.
  const approval = { ...base, writePaths: [...base.writePaths, proofPath] };
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context();
  const input = session.read('owner/input.ts');
  session.write(input.path, input.sha256, input.content + '// candidate change\n');
  // The implementation lands as the candidate commit; the host gate runs on it.
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'implementation candidate');
  session.context();
  assert.equal((await session.verifyGate('acceptance') as any).result, 'passed');
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const envelope = JSON.parse(readFileSync(join(store, receiptPath), 'utf8'));
  const digest = receiptDigest(envelope.payload);
  const candidate = git('rev-parse', 'HEAD').trim();
  assert.equal(envelope.payload.sourceSha, candidate);

  const proof = {
    schemaVersion: 1,
    kind: 'opl.development.stage.proof.v1',
    approval: JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'approval.json'), 'utf8')),
    receipt: envelope,
  };
  put(root, proofPath, JSON.stringify(proof, null, 2));
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'proof-only successor commit');
  const head = git('rev-parse', 'HEAD').trim();
  const authority = readFileSync(join(store, 'authority.pub'), 'utf8');

  const body = render({
    Baseline: `Base SHA: \`${approval.baseSha}\``,
    'Write Set': '- `owner/input.ts`\n- `' + proofPath + '`',
    'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\`; proof: \`${proofPath}\``,
  });
  const facts = {
    plan, root, eventBaseSha: approval.baseSha, headSha: head, mode: 'merge' as const,
    authorityPublicKey: authority,
    changedPaths: ['owner/input.ts', proofPath],
  };
  const result = checkPullRequestBody(body, facts);
  assert.equal(result.ok, true, result.errors.join('\n'));
  assert.equal(result.authorization.basis, 'signed-proof');
  assert.equal(result.authorization.ok, true);

  // A commit that changes anything but the proof after the receipt is not evidence.
  put(root, 'owner/input.ts', 'export const answer = 99;\n');
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'unproven successor change');
  const laterHead = git('rev-parse', 'HEAD').trim();
  const afterReceipt = checkPullRequestBody(body, { ...facts, headSha: laterHead, changedPaths: ['owner/input.ts', proofPath] });
  assert.equal(afterReceipt.ok, false);
  assert.match(afterReceipt.errors.join('\n'), /MERGE_AUTHORIZATION/);

  // A different configured trust root cannot verify the proof.
  const rogue = generateKeyPairSync('ed25519');
  const wrongKey = rogue.publicKey.export({ type: 'spki', format: 'pem' }).toString();
  const wrongAuthority = checkPullRequestBody(body, { ...facts, headSha: laterHead, changedPaths: ['owner/input.ts', proofPath], authorityPublicKey: wrongKey });
  assert.equal(wrongAuthority.ok, false);
  assert.match(wrongAuthority.errors.join('\n'), /signature mismatch/);

  // A self-signed proof whose payload is swapped in cannot pass with the trusted key.
  const rogueEnvelope = { payload: { ...envelope.payload }, signature: sign(null, Buffer.from(canonical(envelope.payload)), rogue.privateKey).toString('base64') };
  put(root, proofPath, JSON.stringify({ ...proof, receipt: rogueEnvelope }));
  const selfSigned = checkPullRequestBody(body, { ...facts, headSha: laterHead, changedPaths: ['owner/input.ts', proofPath] });
  assert.equal(selfSigned.ok, false);
  assert.match(selfSigned.errors.join('\n'), /signature mismatch/);
});

test('merge authorization fails closed without a configured trust root and refuses evidence-layer mixing', async t => {
  const { root, store, git, plan, approval } = fixture(t);
  development.approveRun(root, store, approval);
  const session = new development.DevelopmentSession(root, store, approval.runId);
  session.context(); await session.verifyGate('acceptance');
  const receiptPath = `runs/${approval.runId}/receipts/acceptance-1.json`;
  const envelope = JSON.parse(readFileSync(join(store, receiptPath), 'utf8'));
  const digest = receiptDigest(envelope.payload);
  const proofPath = 'docs/evidence/development-stage/owner-run-acceptance-1.json';
  const base = approval.baseSha;
  const head = git('rev-parse', 'HEAD').trim();
  const stageEntry = `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\`; proof: \`${proofPath}\``;
  const body = render({ Baseline: `Base SHA: \`${base}\``, 'Write Set': `- \`owner/input.ts\`\n- \`${proofPath}\``, 'Receipt Pipeline': stageEntry });

  // No trusted store, no proof: merge-ready is refused, not skipped and not green.
  const unproven = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': `- receipt: development-stage; result: passed; path: \`${receiptPath}\`; run: \`${approval.runId}\`; gate: \`acceptance\`; attempt: 1; sha256: \`${digest}\`` }),
    { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: head, mode: 'merge' },
  );
  assert.equal(unproven.ok, false);
  assert.match(unproven.errors.join('\n'), /MERGE_AUTHORIZATION.*(UNPROVEN|trusted host proof)/);

  // A proof file exists but the trusted host public key is not configured.
  put(root, proofPath, JSON.stringify({ schemaVersion: 1, kind: 'opl.development.stage.proof.v1',
    approval: JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'approval.json'), 'utf8')), receipt: envelope }));
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'proof without configured trust root');
  const proofHead = git('rev-parse', 'HEAD').trim();
  const unconfigured = checkPullRequestBody(body, { plan, root, changedPaths: ['owner/input.ts', proofPath], eventBaseSha: base, headSha: proofHead, mode: 'merge' });
  assert.equal(unconfigured.ok, false);
  assert.match(unconfigured.errors.join('\n'), /MERGE_AUTHORIZATION_UNCONFIGURED/);

  // Evidence-layer mixing: a business-layer receipt cannot be the proof, and a
  // business receipt entry cannot authorize a merge at all.
  // The store author's own key signs a business-layer payload: the layer check
  // must refuse it even though both signatures verify.
  const key = createPrivateKey(readFileSync(join(store, 'authority.key')));
  const mixedPayload = { ...envelope.payload, evidenceLayer: 'business' };
  const mixedProof = { payload: mixedPayload, signature: sign(null, Buffer.from(canonical(mixedPayload)), key).toString('base64') };
  put(root, proofPath, JSON.stringify({ schemaVersion: 1, kind: 'opl.development.stage.proof.v1',
    approval: JSON.parse(readFileSync(join(store, 'runs', approval.runId, 'approval.json'), 'utf8')), receipt: mixedProof }));
  git('add', '.'); git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'layer-mixed proof');
  const mixedHead = git('rev-parse', 'HEAD').trim();
  const authority = readFileSync(join(store, 'authority.pub'), 'utf8');
  const mixed = checkPullRequestBody(body, { plan, root, changedPaths: ['owner/input.ts', proofPath], eventBaseSha: base, headSha: mixedHead, mode: 'merge', authorityPublicKey: authority });
  assert.equal(mixed.ok, false);
  assert.match(mixed.errors.join('\n'), /EVIDENCE_INVALID/);

  const businessOnly = checkPullRequestBody(
    render({ Baseline: `Base SHA: \`${base}\``, 'Receipt Pipeline': '- receipt: business; result: passed; authority: `ledger`; ref: `receipt-1`' }),
    { plan, root, changedPaths: ['owner/input.ts'], eventBaseSha: base, headSha: mixedHead, mode: 'merge', authorityPublicKey: authority },
  );
  assert.equal(businessOnly.ok, false);
  assert.match(businessOnly.errors.join('\n'), /SOURCE_LAYER_EVIDENCE_REQUIRED|MERGE_AUTHORIZATION/);
});

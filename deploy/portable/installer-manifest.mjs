// Shared by the source validators and the distributed installer. Standard Node only.
import { createHash } from 'node:crypto';
import { lstat, readFile, readdir } from 'node:fs/promises';
import { join } from 'node:path';

const SHA = /^[0-9a-f]{40}$/;
const DIGEST = /^sha256:[0-9a-f]{64}$/;
const HASH = /^[0-9a-f]{64}$/;
const DECIMAL = /^[1-9][0-9]*$/;
const REPO = 'gaofeng21cn/one-person-lab-cloud';
const IMAGE = `ghcr.io/${REPO}`;
const PLATFORMS = ['linux/amd64', 'linux/arm64'];
const fail = () => { throw new Error('cloud_installation_bundle_invalid'); };
export const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');
export const exactKeys = (v, keys) => v !== null && typeof v === 'object' && !Array.isArray(v) &&
  JSON.stringify(Object.keys(v).sort()) === JSON.stringify([...keys].sort());
export function canonicalJson(v) {
  if (Array.isArray(v)) return `[${v.map(canonicalJson).join(',')}]`;
  if (v !== null && typeof v === 'object') return `{${Object.keys(v).sort().map(k => `${JSON.stringify(k)}:${canonicalJson(v[k])}`).join(',')}}`;
  const text = JSON.stringify(v);
  if (text === undefined) fail();
  return text;
}

// JSON.parse validates grammar; this second grammar walk also rejects duplicate keys.
export function parseStrictJson(bytes) {
  const text = typeof bytes === 'string' ? bytes : new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  const value = JSON.parse(text);
  const tokens = text.match(/"(?:[^"\\\u0000-\u001f]|\\(?:["\\/bfnrt]|u[0-9a-fA-F]{4}))*"|-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|true|false|null|[{}\[\],:]/g) || [];
  let i = 0;
  function walk(depth) {
    if (depth > 128) fail();
    const token = tokens[i++];
    if (token === '{') {
      const keys = new Set();
      if (tokens[i] === '}') { i++; return; }
      for (;;) {
        const key = JSON.parse(tokens[i++]);
        if (keys.has(key)) fail();
        keys.add(key);
        if (tokens[i++] !== ':') fail();
        walk(depth + 1);
        const end = tokens[i++];
        if (end === '}') break;
        if (end !== ',') fail();
      }
    } else if (token === '[') {
      if (tokens[i] === ']') { i++; return; }
      for (;;) {
        walk(depth + 1);
        const end = tokens[i++];
        if (end === ']') break;
        if (end !== ',') fail();
      }
    } else if (token === undefined) fail();
  }
  walk(0);
  if (i !== tokens.length) fail();
  return value;
}

export function assetNames(contract, format, kind) {
  const spec = contract.bundleFormats.formats[format];
  if (!spec || !['candidate', 'release'].includes(kind)) fail();
  const names = spec[`${kind}Assets`];
  const result = names === 'candidateAssets' ? spec.candidateAssets : names;
  if (!Array.isArray(result) || !result.length || new Set(result).size !== result.length ||
      result.some(name => typeof name !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name))) fail();
  return [...result];
}
export function selectAssetFormat(names, contract, kind) {
  const matches = Object.keys(contract.bundleFormats.formats).filter(format =>
    JSON.stringify(names) === JSON.stringify(assetNames(contract, format, kind)));
  if (matches.length !== 1) fail();
  return matches[0];
}
function validHashes(assets, names) {
  return Array.isArray(assets) && assets.length === names.length && assets.every((a, i) =>
    exactKeys(a, ['name', 'sha256']) && a.name === names[i] && typeof a.sha256 === 'string' && HASH.test(a.sha256));
}
function validPlatforms(entries, indexDigest) {
  return Array.isArray(entries) && entries.length === 2 && entries.every((e, i) =>
    exactKeys(e, ['platform', 'digest']) && e.platform === PLATFORMS[i] && typeof e.digest === 'string' && DIGEST.test(e.digest)) &&
    new Set([indexDigest, ...entries.map(e => e.digest)]).size === 3;
}
export function validateCandidate(value, contract) {
  if (!exactKeys(value, ['schemaVersion', 'kind', 'product', 'cloudImage', 'assets', 'provenance']) ||
      value.schemaVersion !== 2 || value.kind !== 'opl_cloud_candidate') fail();
  const { product: p, cloudImage: c, provenance: r } = value;
  if (!exactKeys(p, ['repository', 'sha', 'tree']) || p.repository !== REPO || !SHA.test(p.sha) || !SHA.test(p.tree) ||
      !exactKeys(c, ['repository', 'indexRef', 'indexDigest', 'revision', 'platforms']) || c.repository !== IMAGE ||
      !DIGEST.test(c.indexDigest) || c.indexRef !== `${IMAGE}@${c.indexDigest}` || c.revision !== p.sha ||
      !validPlatforms(c.platforms, c.indexDigest) || !Array.isArray(value.assets) ||
      !exactKeys(r, ['workflowRepository', 'workflowSha', 'workflowRunId', 'workflowRunAttempt']) ||
      r.workflowRepository !== REPO || !SHA.test(r.workflowSha) || !DECIMAL.test(r.workflowRunId) || !DECIMAL.test(r.workflowRunAttempt)) fail();
  const format = selectAssetFormat(value.assets.map(a => a?.name), contract, 'candidate');
  if (!validHashes(value.assets, assetNames(contract, format, 'candidate'))) fail();
  return value;
}
export function validateRelease(value, contract) {
  const keys = ['schemaVersion','product','productRepository','releaseTag','source','acceptance','candidate','installation','qualifications','assets'];
  if (!value || !Array.isArray(value.assets)) fail();
  const format = selectAssetFormat(value.assets.slice(0, -1), contract, 'release');
  if (format !== 'legacy-v2') keys.push('assetFormat', 'assetDigests');
  const c = value.candidate;
  if (!exactKeys(value, keys) || value.schemaVersion !== 4 || value.product !== 'OPL Cloud' || value.productRepository !== REPO ||
      !/^v0\.[0-9]+\.[0-9]+$/.test(value.releaseTag) || !exactKeys(value.source, ['sha','tree']) ||
      !SHA.test(value.source.sha) || !SHA.test(value.source.tree) || value.acceptance !== 'installation_and_source_paths' ||
      !exactKeys(c, ['workflowRunId','workflowRunAttempt','receiptDigest','image']) ||
      !DECIMAL.test(c.workflowRunId) || !DECIMAL.test(c.workflowRunAttempt) || !DIGEST.test(c.receiptDigest) ||
      !exactKeys(c.image, ['ref','digest','platforms']) || !DIGEST.test(c.image.digest) ||
      c.image.ref !== `${IMAGE}@${c.image.digest}` || !validPlatforms(c.image.platforms, c.image.digest) ||
      value.assets.at(-1) !== contract.bundleFormats.checksumManifest ||
      (format !== 'legacy-v2' && (value.assetFormat !== format || !validHashes(value.assetDigests, assetNames(contract, format, 'release'))))) fail();
  return value;
}
export function releaseAssetNames(manifest, contract) {
  validateRelease(manifest, contract);
  return [...manifest.assets.slice(0, -1), contract.bundleFormats.releaseManifest, contract.bundleFormats.checksumManifest];
}

// Optional contract is for source-side historical validation. Distributed callers
// use the one-argument interface and the contract carried by their exact bundle.
export async function readInstallationBundle(bundleDir, suppliedContract) {
  try {
    if (!(await lstat(bundleDir)).isDirectory()) fail();
    const entries = await readdir(bundleDir, { withFileTypes: true });
    if (entries.some(e => !e.isFile())) fail();
    const contract = suppliedContract ?? parseStrictJson(await readFile(join(bundleDir, 'opl-cloud-distribution-contract.json')));
    const spec = contract.bundleFormats;
    const candidates = entries.filter(e => [spec.candidateManifest, spec.releaseManifest].includes(e.name));
    if (candidates.length !== 1) fail();
    const manifestName = candidates[0].name;
    const isCandidate = manifestName === spec.candidateManifest;
    const bytes = await readFile(join(bundleDir, manifestName));
    const manifest = isCandidate ? validateCandidate(parseStrictJson(bytes), contract) : validateRelease(parseStrictJson(bytes), contract);
    if (isCandidate && bytes.toString('utf8') !== `${canonicalJson(manifest)}\n`) fail();
    const names = isCandidate ? manifest.assets.map(a => a.name) : manifest.assets.slice(0, -1);
    const expected = [...names, manifestName, spec.checksumManifest];
    if (JSON.stringify(entries.map(e => e.name).sort()) !== JSON.stringify(expected.sort())) fail();
    const checksums = await readFile(join(bundleDir, spec.checksumManifest), 'utf8');
    if (!checksums.endsWith('\n')) fail();
    const lines = checksums.slice(0, -1).split('\n');
    const covered = [...names, manifestName];
    if (lines.length !== covered.length) fail();
    const assets = [];
    for (let i = 0; i < covered.length; i++) {
      const match = /^([0-9a-f]{64})  ([A-Za-z0-9][A-Za-z0-9._-]*)$/.exec(lines[i]);
      if (!match || match[2] !== covered[i] || !(await lstat(join(bundleDir, covered[i]))).isFile()) fail();
      const digest = sha256(await readFile(join(bundleDir, covered[i])));
      if (match[1] !== digest) fail();
      if (i < names.length) assets.push({ name: covered[i], sha256: digest });
    }
    const mapping = isCandidate ? manifest.assets : manifest.assetDigests;
    if (mapping && canonicalJson(mapping) !== canonicalJson(assets)) fail();
    const imageRef = isCandidate ? manifest.cloudImage.indexRef : manifest.candidate.image.ref;
    const source = isCandidate ? manifest.product : manifest.source;
    return { manifest, imageRef, sourceSha: source.sha, sourceTree: source.tree, assets };
  } catch {
    fail();
  }
}

#!/usr/bin/env node
/**
 * Measure the repo-scope development ratchet metrics for one-person-lab-cloud.
 *
 * Read-only. Recomputes each metric from repository state on every run so that
 * the ratchet ceiling is compared against a reproducible number rather than a
 * remembered one.
 *
 * Usage:
 *   node scripts/measure-dev-ratchet.mjs --repo <path> --out <file.json>
 *   node scripts/measure-dev-ratchet.mjs --repo <path>          # stdout
 *
 * Emits: { "metric": <count>, ... } for every repo-scope metric declared in
 * .sentrux/rules.toml. A metric declared repo-scope but not emitted here is a
 * measurement bug: the ratchet check will refuse the run rather than pass it.
 */
import { readFileSync, writeFileSync, existsSync, readdirSync, statSync } from 'node:fs';
import { join, resolve, relative, sep } from 'node:path';

const argv = process.argv.slice(2);
const flag = (name, fallback = null) => {
  const at = argv.indexOf(`--${name}`);
  return at === -1 ? fallback : argv[at + 1];
};

const repoRoot = resolve(flag('repo', process.cwd()));
const planPath = flag('plan', join(repoRoot, 'docs/spec/target/checks/development_plan.json'));
const ratchetPath = flag('ratchet', join(repoRoot, 'dev-ratchet.toml'));
const outPath = flag('out', null);

if (!existsSync(planPath)) {
  process.stderr.write(`plan not found: ${planPath}\n`);
  process.exit(2);
}

// --- minimal TOML reader for the [constraints] table this repo uses ---
function parseRatchet(file) {
  const text = readFileSync(file, 'utf8');
  const constraints = {};
  let inTable = false;
  for (const raw of text.split('\n')) {
    const line = raw.replace(/#.*$/u, '').trim();
    if (!line) continue;
    if (line.startsWith('[')) { inTable = line === '[constraints]'; continue; }
    if (!inTable) continue;
    const at = line.indexOf('=');
    if (at === -1) continue;
    const key = line.slice(0, at).trim();
    const value = Number(line.slice(at + 1).trim());
    if (/^[a-z0-9]+(_[a-z0-9]+)*$/u.test(key) && Number.isInteger(value)) constraints[key] = value;
  }
  return constraints;
}

function expandGlob(pattern) {
  // Only `services/*/<suffix>` style single-level globs are admitted here.
  const star = pattern.indexOf('*');
  if (star === -1) return [pattern];
  const head = pattern.slice(0, star).replace(/\/$/u, '');
  const tail = pattern.slice(star + 1).replace(/^\//u, '');
  const base = join(repoRoot, head);
  if (!existsSync(base) || !statSync(base).isDirectory()) return [];
  return readdirSync(base)
    .sort()
    .map((name) => `${head}/${name}${tail ? `/${tail}` : ''}`)
    .filter((candidate) => existsSync(join(repoRoot, candidate)));
}

function normalizePathSet(paths) {
  const out = new Set();
  for (const raw of paths) {
    if (typeof raw !== 'string' || raw.length === 0) continue;
    if (raw.includes('*')) for (const expanded of expandGlob(raw)) out.add(expanded.replace(/\/$/u, ''));
    else out.add(raw.replace(/\/$/u, ''));
  }
  return out;
}

function describesFile(claim, candidate) {
  return claim === candidate || candidate.startsWith(`${claim}/`);
}

function pairwiseConflictCount(slices) {
  let conflicts = 0;
  for (let i = 0; i < slices.length; i += 1) {
    for (let j = i + 1; j < slices.length; j += 1) {
      const a = slices[i].paths;
      const b = slices[j].paths;
      let overlap = false;
      for (const pa of a) {
        for (const pb of b) {
          if (describesFile(pa, pb) || describesFile(pb, pa)) { overlap = true; break; }
        }
        if (overlap) break;
      }
      if (overlap) conflicts += 1;
    }
  }
  return conflicts;
}

// --- metrics ---

function measureSliceMetrics() {
  const plan = JSON.parse(readFileSync(planPath, 'utf8'));
  const raw = Array.isArray(plan.executionSlices) ? plan.executionSlices : [];
  const slices = raw.map((entry) => ({
    id: entry.id ?? '(unnamed)',
    paths: normalizePathSet([...(entry.writePaths ?? [])]),
    testPaths: normalizePathSet([...(entry.testWritePaths ?? [])]),
  }));

  const conflicts = pairwiseConflictCount(slices);

  const ownership = new Map();
  for (const slice of slices) {
    for (const claim of slice.paths) {
      if (!ownership.has(claim)) ownership.set(claim, []);
      ownership.get(claim).push(slice.id);
    }
  }

  const withoutExclusive = slices.filter((slice) =>
    slice.paths.size > 0 &&
    [...slice.paths].every((claim) => (ownership.get(claim) ?? []).length > 1),
  ).length;

  const slicesWithoutMachineVerification = slices.filter((slice) => slice.testPaths.size === 0).length;

  return {
    slice_writeset_conflicts: conflicts,
    slices_without_exclusive_writes: withoutExclusive,
    slices_without_machine_verification: slicesWithoutMachineVerification,
  };
}

function measureCoverageMetrics() {
  // Schema documents that do not declare which validator enforces them can
  // never be shown to be enforced.
  const schemaDir = join(repoRoot, 'schemas');
  if (!existsSync(schemaDir)) return { schemas_without_validator_codes: 0 };
  const files = readdirSync(schemaDir).filter((n) => n.endsWith('.json'));
  const missing = files.filter((name) => {
    const doc = JSON.parse(readFileSync(join(schemaDir, name), 'utf8'));
    return !(doc['x-validator'] || {}).codes;
  }).length;
  return { schemas_without_validator_codes: missing };
}

const declared = parseRatchet(ratchetPath);
const measured = { ...measureSliceMetrics(), ...measureCoverageMetrics() };

// Only repo-scope metrics are emitted here. local-scope metrics (worktrees,
// local branches) are workstation state and are never invented on a CI runner.
// TOML declares ceilings as `max_<metric>`; the measurement is `<metric>`.
const declaredMetrics = new Set(
  (declared ? Object.keys(declared) : []).map((key) => key.replace(/^max_/u, '')),
);
const emitted = Object.fromEntries(
  Object.entries(measured).filter(([metric]) => declaredMetrics.has(metric)),
);

const serialised = `${JSON.stringify(emitted, null, 2)}\n`;
if (outPath) {
  writeFileSync(outPath, serialised);
  process.stderr.write(`wrote ${Object.keys(emitted).length} measurements to ${outPath}\n`);
} else {
  process.stdout.write(serialised);
}
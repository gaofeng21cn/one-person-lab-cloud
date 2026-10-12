#!/usr/bin/env node
/**
 * Check a measurement set against the development ratchet.
 *
 * The measuring process and this check stay separate on purpose: a check that
 * shares the measuring process cannot tell a genuine regression from a change
 * in how the number was produced.
 *
 * Usage:
 *   node scripts/check-dev-ratchet.mjs \
 *     --framework-root <checkout of opl-development-framework> \
 *     --ratchet <path to rules> \
 *     --measurements <path to measurements.json> \
 *     [--previous <path to previous ceilings json>]
 *
 * Exit codes:
 *   0  every declared repo-scope constraint is measured and within its ceiling
 *   1  a regression, a loosened ceiling, an unmeasured repo metric, or bad shape
 *   2  the invocation itself could not be completed
 */
import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const argv = process.argv.slice(2);
const flag = (name) => {
  const at = argv.indexOf(`--${name}`);
  return at === -1 ? undefined : argv[at + 1];
};

const frameworkRoot = flag('framework-root');
const ratchetPath = flag('ratchet');
const measurementsPath = flag('measurements');
const previousPath = flag('previous');

for (const [name, value] of Object.entries({ 'framework-root': frameworkRoot, ratchet: ratchetPath, measurements: measurementsPath })) {
  if (!value) {
    process.stderr.write(`missing --${name}\n`);
    process.exit(2);
  }
}

const modulePath = join(resolve(frameworkRoot), 'validators/lib/ratchet.mjs');
let evaluateRatchet;
let ratchetHeadroom;
try {
  ({ evaluateRatchet, ratchetHeadroom } = await import(pathToFileURL(modulePath).href));
} catch (error) {
  process.stderr.write(`cannot load ratchet validator from ${modulePath}\n  ${error.message}\n`);
  process.exit(2);
}

// The TOML file carries ceilings as `max_<metric> = <n>`; the validator expects
// {max, scope} per metric. Only repo-scope metrics are enforced here, matching
// the measure step, which never invents a workstation-only number.
function readRatchetDocument(file) {
  const constraints = {};
  let inTable = false;
  for (const raw of readFileSync(file, 'utf8').split('\n')) {
    const line = raw.replace(/#.*$/u, '').trim();
    if (!line) continue;
    if (line.startsWith('[')) { inTable = line === '[constraints]'; continue; }
    if (!inTable) continue;
    const at = line.indexOf('=');
    if (at === -1) continue;
    const key = line.slice(0, at).trim();
    const value = Number(line.slice(at + 1).trim());
    if (!key.startsWith('max_')) continue;
    if (!Number.isInteger(value)) continue;
    constraints[key.slice(4)] = { max: value, scope: 'repo' };
  }
  if (Object.keys(constraints).length === 0) {
    process.stderr.write(`no max_<metric> constraints found under [constraints] in ${file}\n`);
    process.exit(2);
  }
  return { kind: 'ratchet', version: 1, constraints };
}

const ratchet = readRatchetDocument(resolve(ratchetPath));
const measurements = JSON.parse(readFileSync(resolve(measurementsPath), 'utf8'));
const previous = previousPath ? JSON.parse(readFileSync(resolve(previousPath), 'utf8')) : undefined;

const verdict = evaluateRatchet({ ratchet, measurements, previous });

if (!verdict.valid) {
  process.stderr.write(`ratchet: ${verdict.code}\n  ${verdict.detail}\n`);
  process.stderr.write('  the ceiling may only be lowered. If the metric is genuinely wrong, fix the\n');
  process.stderr.write('  measurement first. If the rule itself should change, that is a separate,\n');
  process.stderr.write('  reviewed edit to the ratchet document.\n');
  process.exit(1);
}

for (const row of ratchetHeadroom({ ratchet, measurements })) {
  process.stderr.write(`ok  ${row.metric}  ${row.measured}/${row.ceiling}  headroom ${row.headroom}\n`);
}
process.exit(0);
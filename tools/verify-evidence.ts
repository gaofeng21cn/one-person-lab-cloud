/**
 * L0 evidence-layer gate.
 *
 * The one failure mode this exists to stop: a claim that reads like proof but
 * proves nothing. Three concrete shapes of it were found in this repository:
 *
 *   1. `docs/evidence/source-checks/` had 253 receipts, zero tests reading them
 *      and zero CI checks linting them. Any JSON with `"result": "pass"` was
 *      admissible evidence for anything, forever.
 *   2. 57 of those receipts wrote English prose into `result`, for example
 *      `"existing_contract_adapter_implemented_real_tke_not_run"`. The field is
 *      an outcome, not a paragraph, so free text is rejected outright.
 *   3. 120 receipts predate the schema entirely. They are grandfathered by an
 *      explicit committed manifest rather than rewritten: adding `result: pass`
 *      and `evidenceLayer: source` to a file that says the real run never
 *      happened would make weak evidence look stronger. Moving it keeps the
 *      original bytes and the original admission.
 *
 * Design constraints, each one a bug that already happened here:
 *
 *   - NO VACUOUS PASS. `pullRequestChangedPaths` returns an empty list when the
 *     base SHA is stale, and an empty list yields zero steps and a green run.
 *     So an empty evidence set is a FAILURE here. Deleting every receipt must
 *     never turn this gate green.
 *   - NO BYPASS FLAG. Nine one-shot `ci(candidate): admit the <sha> bridge ref`
 *     workflows were opened in 2026-10-10 to pin a repository, a ref and a
 *     product SHA into a `workflow_dispatch` escape hatch. This tool therefore
 *     takes no arguments and has no force/skip/allow flag. There is no lever.
 *   - THE LAYER IS LOAD-BEARING. `source` evidence never satisfies a claim that
 *     a runtime behaviour works. A `real_env` receipt without an external
 *     readback is refused, because "I ran it" is the claim that has been false
 *     before.
 */

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const evidenceDirectory = "docs/evidence/source-checks";
export const legacyManifestPath = "docs/evidence/source-checks-legacy.json";

/**
 * Ordered weakest to strongest. A receipt names exactly one layer; the layer
 * bounds what the receipt may be used to claim, and nothing upgrades it.
 */
export const evidenceLayers = ["source", "source_and_browser_fixture", "real_env"] as const;
export type EvidenceLayer = (typeof evidenceLayers)[number];

/** A closed set. Anything else is prose in a field that must hold an outcome. */
export const evidenceResults = ["pass", "fail", "partial"] as const;
export type EvidenceResult = (typeof evidenceResults)[number];

/**
 * `real_env` is the only layer that may close a runtime claim, and only when an
 * owner outside this repository confirmed the effect. The readback names that
 * observer so a receipt cannot claim external confirmation without saying what
 * was observed and where.
 */
export const realEnvReadbackKinds = ["owner_api", "cloud_control_plane", "public_endpoint", "ledger_readback"] as const;

export type RealEnvReadback = {
  kind: (typeof realEnvReadbackKinds)[number];
  target: string;
  observedAt: string;
};

export type Receipt = {
  schemaVersion?: unknown;
  receiptType?: unknown;
  evidenceLayer?: unknown;
  result?: unknown;
  sourceBaseSha?: unknown;
  recordedAt?: unknown;
  writeSet?: unknown;
  verifiedSource?: unknown;
  execution?: unknown;
  sourceStage?: unknown;
  steps?: unknown;
  realEnvReadback?: unknown;
};

/**
 * Fields every receipt must carry: what ran, against what tree, with what
 * outcome.
 */
const commonRequiredFields = [
  "schemaVersion", "receiptType", "evidenceLayer", "result", "sourceBaseSha", "recordedAt"
] as const;

/**
 * Fields a `source` receipt must additionally carry. These describe a run of a
 * repository gate against a code change, so they only mean something when a
 * dev-session run is behind the receipt.
 *
 * They are NOT required of a `real_env` receipt, and that is not a weakening: a
 * real-environment receipt has no dev-session run behind it, because it
 * observes an external system rather than gating a diff. The equivalent
 * guarantee is carried instead by `steps[]` (each with a command and an exit
 * code) plus `realEnvReadback[]` (each naming an outside observer). See
 * `requiredFieldsFor` and the negative controls in the test file.
 */
const sourceRequiredFields = ["writeSet", "verifiedSource", "execution", "sourceStage"] as const;

/** Fields a `real_env` receipt must carry instead. */
const realEnvRequiredFields = ["steps", "realEnvReadback"] as const;

export function requiredFieldsFor(evidenceLayer: unknown): readonly string[] {
  if (evidenceLayer === "real_env") return [...commonRequiredFields, ...realEnvRequiredFields];
  return [...commonRequiredFields, ...sourceRequiredFields];
}

const hexSha = /^[0-9a-f]{40}$/u;
const isoTimestamp = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/u;

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Validate one receipt against the schema the host export writes in
 * `tools/dev-session.ts`. Returns every violation rather than the first, so a
 * repair pass sees the whole defect list.
 */
export function classifyReceipt(id: string, record: unknown): string[] {
  const errors: string[] = [];
  if (!isPlainObject(record)) return [`${id}: not a JSON object`];
  const receipt = record as Receipt;

  for (const field of requiredFieldsFor(receipt.evidenceLayer)) {
    if (receipt[field] === undefined) errors.push(`${id}: missing required field ${field}`);
  }

  if (typeof receipt.evidenceLayer === "string" && !evidenceLayers.includes(receipt.evidenceLayer as EvidenceLayer)) {
    errors.push(`${id}: evidenceLayer "${receipt.evidenceLayer}" is not one of ${evidenceLayers.join(" | ")}`);
  }

  // The load-bearing check. A receipt whose result is a sentence is a receipt
  // whose result is a claim.
  if (receipt.result !== undefined && typeof receipt.result !== "string") {
    errors.push(`${id}: result must be a string`);
  } else if (typeof receipt.result === "string" && !evidenceResults.includes(receipt.result as EvidenceResult)) {
    const shape = receipt.result.length > 40 ? ` (${receipt.result.length} chars of prose)` : "";
    errors.push(`${id}: result "${receipt.result.slice(0, 60)}" is not one of ${evidenceResults.join(" | ")}${shape}`);
  }

  if (typeof receipt.sourceBaseSha === "string" && !hexSha.test(receipt.sourceBaseSha)) {
    errors.push(`${id}: sourceBaseSha is not a 40-character Git SHA`);
  }
  if (typeof receipt.recordedAt === "string" && !isoTimestamp.test(receipt.recordedAt)) {
    errors.push(`${id}: recordedAt is not an ISO-8601 timestamp`);
  }

  if (receipt.writeSet !== undefined && !Array.isArray(receipt.writeSet)) {
    errors.push(`${id}: writeSet must be an array`);
  }
  if (receipt.verifiedSource !== undefined && !isPlainObject(receipt.verifiedSource)) {
    errors.push(`${id}: verifiedSource must be an object`);
  }

  if (receipt.execution !== undefined) {
    if (!isPlainObject(receipt.execution)) {
      errors.push(`${id}: execution must be an object`);
    } else {
      const execution = receipt.execution as Record<string, unknown>;
      // A receipt that records no command proves nothing ran.
      if (typeof execution.command !== "string" || !execution.command.trim()) {
        errors.push(`${id}: execution.command must record the command that ran`);
      }
      if (typeof execution.exitCode !== "number") {
        errors.push(`${id}: execution.exitCode must be a number`);
      }
    }
  }

  if (receipt.sourceStage !== undefined && !isPlainObject(receipt.sourceStage)) {
    errors.push(`${id}: sourceStage must be an object`);
  }

  // A real-environment receipt is the strongest thing this repository can
  // produce, so it carries the strictest requirement: somebody outside the test
  // must have observed the effect. `steps` is the layer-appropriate stand-in for
  // a source receipt's `execution` block: each entry must record a command and
  // its exit code, so "it worked" is always traceable to something that ran.
  if (receipt.evidenceLayer === "real_env") {
    const steps = receipt.steps;
    if (!Array.isArray(steps) || steps.length === 0) {
      errors.push(`${id}: evidenceLayer real_env requires a non-empty steps array`);
    } else {
      steps.forEach((entry, position) => {
        if (!isPlainObject(entry)) {
          errors.push(`${id}: steps[${position}] must be an object`);
          return;
        }
        const { command, exitCode } = entry as Record<string, unknown>;
        if (typeof command !== "string" || !command.trim()) {
          errors.push(`${id}: steps[${position}].command must record what was run`);
        }
        if (typeof exitCode !== "number") {
          errors.push(`${id}: steps[${position}].exitCode must be a number`);
        }
      });
    }

    const readback = receipt.realEnvReadback;
    if (!Array.isArray(readback) || readback.length === 0) {
      errors.push(`${id}: evidenceLayer real_env requires a non-empty realEnvReadback`);
    } else {
      readback.forEach((entry, position) => {
        if (!isPlainObject(entry)) {
          errors.push(`${id}: realEnvReadback[${position}] must be an object`);
          return;
        }
        const { kind, target, observedAt } = entry as Record<string, unknown>;
        if (typeof kind !== "string" || !realEnvReadbackKinds.includes(kind as (typeof realEnvReadbackKinds)[number])) {
          errors.push(`${id}: realEnvReadback[${position}].kind must be one of ${realEnvReadbackKinds.join(" | ")}`);
        }
        if (typeof target !== "string" || !target.trim()) {
          errors.push(`${id}: realEnvReadback[${position}].target must name what was observed`);
        }
        if (typeof observedAt !== "string" || !isoTimestamp.test(observedAt)) {
          errors.push(`${id}: realEnvReadback[${position}].observedAt must be an ISO-8601 timestamp`);
        }
      });
    }
  }

  return errors;
}

export type EvidenceReport = {
  passed: boolean;
  errors: string[];
  counts: { total: number; conformant: number; grandfathered: number; failing: number };
  layers: Record<string, number>;
  results: Record<string, number>;
};

export function readLegacyManifest(root: string): Set<string> {
  const manifest = join(root, legacyManifestPath);
  if (!existsSync(manifest)) return new Set();
  const parsed = JSON.parse(readFileSync(manifest, "utf8"));
  if (!isPlainObject(parsed) || !Array.isArray(parsed.receipts)) {
    throw new Error(`${legacyManifestPath} must be an object with a receipts array`);
  }
  return new Set(parsed.receipts.map(String));
}

export function verifyEvidence(root = repositoryRoot): EvidenceReport {
  const directory = join(root, evidenceDirectory);
  const errors: string[] = [];
  const counts = { total: 0, conformant: 0, grandfathered: 0, failing: 0 };
  const layers: Record<string, number> = {};
  const results: Record<string, number> = {};

  const legacy = readLegacyManifest(root);

  if (!existsSync(directory)) {
    // Fail closed rather than skip. A missing directory is not a clean slate.
    return {
      passed: false,
      errors: [`missing evidence directory ${evidenceDirectory}`],
      counts: { ...counts },
      layers, results
    };
  }

  const files = readdirSync(directory).filter(name => name.endsWith(".json")).sort();

  // The vacuous-pass guard. See the header: an empty set must be red.
  if (files.length === 0) {
    return {
      passed: false,
      errors: [`${evidenceDirectory} is empty: an empty evidence set must fail, not pass`],
      counts: { ...counts },
      layers, results
    };
  }

  for (const file of files) {
    counts.total += 1;
    let record: unknown;
    try {
      record = JSON.parse(readFileSync(join(directory, file), "utf8"));
    } catch (error) {
      counts.failing += 1;
      errors.push(`${file}: invalid JSON (${error instanceof Error ? error.message : String(error)})`);
      continue;
    }

    if (isPlainObject(record)) {
      const layer = String(record.evidenceLayer ?? "(absent)");
      const result = String(record.result ?? "(absent)");
      layers[layer] = (layers[layer] ?? 0) + 1;
      results[result.length > 40 ? "(prose)" : result] = (results[result.length > 40 ? "(prose)" : result] ?? 0) + 1;
    }

    const receiptErrors = classifyReceipt(file, record);
    if (receiptErrors.length === 0) {
      counts.conformant += 1;
      continue;
    }
    if (legacy.has(file)) {
      counts.grandfathered += 1;
      continue;
    }
    counts.failing += 1;
    errors.push(...receiptErrors);
  }

  return { passed: errors.length === 0, errors, counts, layers, results };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    if (process.argv.length !== 2) throw new Error("verify:evidence accepts no arguments");
    const report = verifyEvidence();
    console.log(`Evidence receipts: ${report.counts.total} total, ${report.counts.conformant} conformant, `
      + `${report.counts.grandfathered} grandfathered, ${report.counts.failing} failing`);
    for (const [layer, count] of Object.entries(report.layers).sort((a, b) => b[1] - a[1])) {
      console.log(`  layer ${layer}: ${count}`);
    }
    for (const [result, count] of Object.entries(report.results).sort((a, b) => b[1] - a[1])) {
      console.log(`  result ${result}: ${count}`);
    }
    for (const error of report.errors) console.error(`FAIL: ${error}`);
    console.log(`Evidence layer gate: ${report.passed ? "PASS" : "FAIL"}`);
    process.exitCode = report.passed ? 0 : 1;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
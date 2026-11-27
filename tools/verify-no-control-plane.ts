#!/usr/bin/env node
/**
 * The retirement yardstick for `services/control-plane`.
 *
 * The Control Plane retirement is a sequence of slices. Each slice moves one
 * owner fact out of the retired service, and every slice needs a way to prove
 * that it left no residue behind. This script is that instrument: it declares
 * one assertion per retirement slice and runs the assertions whose slice has
 * arrived.
 *
 * The assertions are data, not scattered conditionals, so the stage table can
 * be checked against the assertion list. An assertion whose `fromSlice` has
 * not arrived yet is skipped instead of failed, and the script prints how many
 * assertions are still pending, so "not checked yet" stays visibly different
 * from "checked and green". The stage table is itself checked: an assertion
 * whose `fromSlice` no longer matches the stage that declares it fails the
 * script, so nobody can quietly push a check into the future.
 *
 * The scope of every scan is declared in the assertion itself. The
 * documentation layer is not part of this gate: it records the retirement in
 * prose, and the documentation owner keeps it. This file is excluded from its
 * own scans because it has to declare the literals it hunts for.
 */

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** This file declares the literals it hunts for, so it never scans itself. */
const SELF_PATH = "tools/verify-no-control-plane.ts";

/**
 * The retirement slices in order. A slice opens its own assertions by raising
 * `currentSlice`; the assertions of every later slice stay pending.
 */
export const sliceOrder = ["S2", "S3", "S4", "S5", "S6", "S7", "S8", "S9", "S10"] as const;
export type Slice = (typeof sliceOrder)[number];

/**
 * The slice this script is delivered in. The retirement slices that follow
 * raise it to their own id; that is the only change a slice needs to open its
 * assertion.
 */
export const currentSlice: Slice = "S2";

/**
 * The declared stage table: the assertion ids each slice turns on. It is the
 * second copy of the `fromSlice` markers, and the script refuses to run when
 * the two disagree.
 */
export const stageTable = Object.freeze({
  S2: [],
  S3: ["bridges-14-15-16-absent"],
  S4: ["legacy-billing-bridges-absent"],
  S5: ["operator-read-bridges-absent"],
  S6: ["announcement-bridges-absent"],
  S7: ["local-docker-gateway-not-control-plane"],
  S8: ["deployment-surface-clean"],
  S9: ["image-pipeline-clean"],
  S10: [
    "control-plane-directory-absent",
    "verify-local-targets-clean",
    "compose-no-root-user",
    "control-plane-env-vars-absent",
    "internal-cloud-prefix-absent"
  ]
} satisfies Record<Slice, readonly string[]>);

export type Finding = { file: string; line: number; text: string };
export type AssertionOutcome = { findings: Finding[]; detail: string };
export type Assertion = {
  id: string;
  fromSlice: Slice;
  description: string;
  scope: string;
  check: (root: string) => AssertionOutcome;
};

/**
 * Directories that hold no repository source: build output, dependency trees,
 * caches and VCS metadata. A symlink is never followed either, so a linked
 * dependency tree is never scanned as repository content.
 */
const skippedDirectories = new Set([
  ".codegraph", ".git", ".gstack", ".runtime", ".worktrees",
  "__pycache__", "dist", "node_modules", "output", "tmp"
]);

/** Every file of the tree, as a repository-relative path. */
export function repositoryFiles(root: string): string[] {
  const files: string[] = [];
  const walk = (directory: string) => {
    let entries;
    try {
      entries = readdirSync(directory, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      if (entry.isSymbolicLink()) continue;
      const absolute = join(directory, entry.name);
      if (entry.isDirectory()) {
        if (skippedDirectories.has(entry.name)) continue;
        walk(absolute);
        continue;
      }
      if (!entry.isFile()) continue;
      files.push(relative(root, absolute).split(sep).join("/"));
    }
  };
  walk(root);
  return files.sort();
}

/**
 * The bridge implementation surface. The deployment surface is excluded
 * because the deployment-surface assertion owns it, and the documentation is
 * excluded because the documentation owner owns it.
 */
function codeSurfaceFiles(root: string): string[] {
  return repositoryFiles(root).filter((path) =>
    path !== SELF_PATH && !path.startsWith("docs/") && !path.startsWith("deploy/"));
}

/** The whole repository surface except the documentation layer and this file. */
function repositorySurfaceFiles(root: string): string[] {
  return repositoryFiles(root).filter((path) => path !== SELF_PATH && !path.startsWith("docs/"));
}

function readTextFile(root: string, path: string): string | undefined {
  let bytes: Buffer;
  try {
    bytes = readFileSync(join(root, path));
  } catch {
    return undefined;
  }
  // A binary file has no literal to find and no line to report.
  if (bytes.includes(0)) return undefined;
  return bytes.toString("utf8");
}

/**
 * Every line that contains `literal`.
 */
function scanLiteral(root: string, files: readonly string[], literal: string): Finding[] {
  const findings: Finding[] = [];
  for (const file of files) {
    const text = readTextFile(root, file);
    if (text === undefined) continue;
    const lines = text.split("\n");
    for (let index = 0; index < lines.length; index += 1) {
      const line = lines[index];
      if (!line.includes(literal)) continue;
      findings.push({ file, line: index + 1, text: line.trim().slice(0, 200) });
    }
  }
  return findings;
}

/** Every line that satisfies `accept`. */
function scanLines(root: string, files: readonly string[], accept: (line: string) => boolean): Finding[] {
  const findings: Finding[] = [];
  for (const file of files) {
    const text = readTextFile(root, file);
    if (text === undefined) continue;
    const lines = text.split("\n");
    for (let index = 0; index < lines.length; index += 1) {
      const line = lines[index];
      if (!accept(line)) continue;
      findings.push({ file, line: index + 1, text: line.trim().slice(0, 200) });
    }
  }
  return findings;
}

const TOPOLOGY_CONTRACT = "deploy/portable/opl-cloud-owner-topology.json";

/**
 * Every value of the owner topology contract that names the Control Plane.
 * The publisher binary list is excluded on request: the image pipeline owns
 * the product image's own binary list, and that slice asserts it separately.
 */
function topologyContractFindings(root: string, options: { excludePublisherBinaryList: boolean }): Finding[] {
  const text = readTextFile(root, TOPOLOGY_CONTRACT);
  if (text === undefined) return [];
  const findings: Finding[] = [];
  const visit = (node: unknown, jsonPath: string) => {
    if (typeof node === "string") {
      if (!/control[-_]plane/iu.test(node)) return;
      if (options.excludePublisherBinaryList && jsonPath.startsWith("/image/defaultCommand")) return;
      findings.push({ file: `${TOPOLOGY_CONTRACT}#${jsonPath}`, line: 0, text: node.slice(0, 200) });
      return;
    }
    if (Array.isArray(node)) {
      node.forEach((item, index) => visit(item, `${jsonPath}/${index}`));
      return;
    }
    if (node && typeof node === "object") {
      for (const [key, value] of Object.entries(node)) visit(value, `${jsonPath}/${key}`);
    }
  };
  visit(JSON.parse(text), "");
  return findings;
}

/**
 * The owner topology contract's publisher binary list: the executables the
 * product image ships as the published default command.
 */
function publisherBinaryListFindings(root: string): Finding[] {
  const text = readTextFile(root, TOPOLOGY_CONTRACT);
  if (text === undefined) return [];
  const contract = JSON.parse(text) as { image?: { defaultCommand?: unknown } };
  const commands = Array.isArray(contract.image?.defaultCommand) ? contract.image!.defaultCommand : [];
  return commands
    .filter((command): command is string => typeof command === "string" && /control[-_]plane/iu.test(command))
    .map((command) => ({ file: `${TOPOLOGY_CONTRACT}#/image/defaultCommand`, line: 0, text: command }));
}

/**
 * Every Dockerfile instruction that names the Control Plane. A Dockerfile
 * instruction is a logical line: a `RUN` with line continuations is one
 * instruction, and a comment is not an instruction.
 */
function dockerfileInstructionFindings(root: string, path: string): Finding[] {
  const text = readTextFile(root, path);
  if (text === undefined) return [];
  const findings: Finding[] = [];
  const lines = text.split("\n");
  let logical = "";
  let start = 0;
  const check = () => {
    const trimmed = logical.trim();
    if (trimmed && !trimmed.startsWith("#") && /control[-_]plane/iu.test(trimmed)) {
      findings.push({ file: path, line: start, text: trimmed.slice(0, 200) });
    }
    logical = "";
  };
  for (let index = 0; index < lines.length; index += 1) {
    if (!logical) start = index + 1;
    const line = lines[index];
    const continued = line.trimEnd().endsWith("\\");
    logical += continued ? line.trimEnd().slice(0, -1) : line;
    if (continued) continue;
    check();
  }
  if (logical) check();
  return findings;
}

/** The published portable deployment surface this assertion owns. */
const DEPLOYMENT_SURFACE = [
  TOPOLOGY_CONTRACT,
  "deploy/portable/compose.cloud.yaml",
  "deploy/portable/install.mjs",
  "deploy/portable/cloud-proxy.nginx.conf",
  "deploy/portable/local-v2-proxy.nginx.conf"
] as const;

/**
 * The operator read bridges. Seven of the eight own a path that no other
 * bridge uses. The accounts bridge shares its path with the retained operator
 * account write, so the path alone cannot be its evidence: a check on the path
 * would also report the retained write bridge and could never turn green. The
 * read bridge is identified by its read-side forms instead: the paginated
 * client construction that calls it, the Control Plane read route table entry
 * that serves it, and a read request for it.
 */
const OPERATOR_READ_BRIDGE_PATHS = [
  "/internal/cloud/operator/overview",
  "/internal/cloud/operator/announcements",
  "/internal/cloud/operator/health",
  "/internal/cloud/operator/runtime-observations",
  "/internal/cloud/operator/reconciliation",
  "/internal/cloud/operator/workspace-launches/",
  "/internal/cloud/operator/wallet-adjustments/"
] as const;

const OPERATOR_ACCOUNTS_READ_FORMS = [
  "\"/internal/cloud/operator/accounts?\" + url.Values",
  "\"/internal/cloud/operator/accounts\", pagination",
  "http.MethodGet, \"/internal/cloud/operator/accounts"
] as const;

export const assertions: readonly Assertion[] = Object.freeze([
  {
    id: "bridges-14-15-16-absent",
    fromSlice: "S3",
    description: "The three operator bridges that have no consumer are gone, together with the BFF public paths that only existed to serve them.",
    scope: "repository code surface (docs/ and deploy/ excluded)",
    check: (root) => {
      const files = codeSurfaceFiles(root);
      const findings = [
        ...scanLiteral(root, files, "/internal/cloud/operator/application-deployments/"),
        ...scanLiteral(root, files, "/internal/cloud/operator/registry/repositories"),
        ...scanLiteral(root, files, "/internal/cloud/operator/registry/tags/"),
        ...scanLiteral(root, files, "/api/v2/operator/application-deployments/"),
        ...scanLiteral(root, files, "/api/v2/operator/registry/repositories"),
        ...scanLiteral(root, files, "/api/v2/operator/registry/tags/")
      ];
      return { findings, detail: `${findings.length} occurrence(s) of the three unconsumed operator bridge paths` };
    }
  },
  {
    id: "legacy-billing-bridges-absent",
    fromSlice: "S4",
    description: "The three legacy-billing bridges are gone from the bridge implementation surface.",
    scope: "repository code surface (docs/ and deploy/ excluded)",
    check: (root) => {
      const files = codeSurfaceFiles(root);
      const findings = scanLiteral(root, files, "/internal/cloud/legacy-billing/");
      return { findings, detail: `${findings.length} occurrence(s) of the legacy-billing bridge paths` };
    }
  },
  {
    id: "operator-read-bridges-absent",
    fromSlice: "S5",
    description: "The eight operator read bridges are gone from the bridge implementation surface. The accounts bridge shares its path with the retained operator account write, so only the read forms of that path count.",
    scope: "repository code surface (docs/ and deploy/ excluded)",
    check: (root) => {
      const files = codeSurfaceFiles(root);
      const findings = [
        ...OPERATOR_READ_BRIDGE_PATHS.map((path) => scanLiteral(root, files, path)),
        ...OPERATOR_ACCOUNTS_READ_FORMS.map((form) => scanLiteral(root, files, form))
      ].flat();
      return { findings, detail: `${findings.length} occurrence(s) of the eight operator read bridge paths` };
    }
  },
  {
    id: "announcement-bridges-absent",
    fromSlice: "S6",
    description: "The two announcement bridges are gone from the bridge implementation surface.",
    scope: "repository code surface (docs/ and deploy/ excluded)",
    check: (root) => {
      const files = codeSurfaceFiles(root);
      const findings = scanLiteral(root, files, "/internal/cloud/announcements");
      return { findings, detail: `${findings.length} occurrence(s) of the announcement bridge paths` };
    }
  },
  {
    id: "local-docker-gateway-not-control-plane",
    fromSlice: "S7",
    description: "The Local-Docker gateway is no longer labelled as the Control Plane, in the Fabric source and in the deployment files that set the label.",
    scope: "repository surface (docs/ excluded)",
    check: (root) => {
      const files = repositorySurfaceFiles(root);
      const findings = scanLines(root, files, (line) =>
        line.includes("opl.fabric.local-docker.gateway") && line.includes("control-plane"));
      return { findings, detail: `${findings.length} line(s) that label the Local-Docker gateway as control-plane` };
    }
  },
  {
    id: "deployment-surface-clean",
    fromSlice: "S8",
    description: "The published portable deployment surface carries no Control Plane entry: no owner process, edge, readiness criterion, PKI identity or proxy upstream. The product image's own publisher binary list is left to the image pipeline assertion.",
    scope: "published portable deployment surface (topology contract, published compose, installer, proxy configuration)",
    check: (root) => {
      const findings = [
        ...topologyContractFindings(root, { excludePublisherBinaryList: true }),
        ...DEPLOYMENT_SURFACE.filter((path) => path !== TOPOLOGY_CONTRACT)
          .flatMap((path) => [
            ...scanLiteral(root, [path], "control-plane"),
            ...scanLiteral(root, [path], "control_plane")
          ])
      ];
      return { findings, detail: `${findings.length} Control Plane entry/entries in the deployment surface` };
    }
  },
  {
    id: "image-pipeline-clean",
    fromSlice: "S9",
    description: "The product image pipeline no longer builds or starts the Control Plane, and the owner topology contract's publisher binary list no longer names it.",
    scope: "product Dockerfile and the owner topology contract's publisher binary list",
    check: (root) => {
      const findings = [
        ...dockerfileInstructionFindings(root, "Dockerfile"),
        ...publisherBinaryListFindings(root)
      ];
      return { findings, detail: `${findings.length} Control Plane build or publish entry/entries` };
    }
  },
  {
    id: "control-plane-directory-absent",
    fromSlice: "S10",
    description: "The Control Plane service directory does not exist.",
    scope: "services/control-plane",
    check: (root) => {
      const path = "services/control-plane";
      const findings = existsSync(join(root, path))
        ? [{ file: path, line: 0, text: "the Control Plane service directory still exists" }]
        : [];
      return { findings, detail: findings.length === 0 ? "the Control Plane service directory is absent" : "the Control Plane service directory still exists" };
    }
  },
  {
    id: "verify-local-targets-clean",
    fromSlice: "S10",
    description: "No local verification target still points at the Control Plane service directory.",
    scope: "tools/verify-local.ts",
    check: (root) => {
      const findings = scanLiteral(root, ["tools/verify-local.ts"], "services/control-plane");
      return { findings, detail: `${findings.length} verification target(s) still naming services/control-plane` };
    }
  },
  {
    id: "compose-no-root-user",
    fromSlice: "S10",
    description: "No compose file starts a service as root.",
    scope: "every compose file in the repository",
    check: (root) => {
      const files = repositoryFiles(root).filter((path) => /^compose[^/]*\.ya?ml$/u.test(path.split("/").pop() ?? ""));
      const findings = scanLiteral(root, files, "user: root");
      return { findings, detail: `${findings.length} compose service(s) still run as root` };
    }
  },
  {
    id: "control-plane-env-vars-absent",
    fromSlice: "S10",
    description: "The Control Plane environment variables and the Control Plane binary name are gone from the repository.",
    scope: "repository surface (docs/ excluded)",
    check: (root) => {
      const files = repositorySurfaceFiles(root);
      const literals = ["OPL_CONTROL_PLANE_URL", "OPL_CONTROL_PLANE_TOKEN", "OPL_CONTROL_PLANE_DATABASE_PASSWORD", "opl-control-plane"];
      const findings = literals.flatMap((literal) => scanLiteral(root, files, literal));
      return { findings, detail: `${findings.length} occurrence(s) of the Control Plane environment variables and binary name` };
    }
  },
  {
    id: "internal-cloud-prefix-absent",
    fromSlice: "S10",
    description: "The retired internal bridge prefix is gone from the repository.",
    scope: "repository surface (docs/ excluded)",
    check: (root) => {
      const files = repositorySurfaceFiles(root);
      const findings = scanLiteral(root, files, "/internal/cloud/");
      return { findings, detail: `${findings.length} occurrence(s) of the internal bridge prefix` };
    }
  }
]);

function sliceIndex(slice: string): number {
  return sliceOrder.indexOf(slice as Slice);
}

/**
 * The meta-assertion. The stage table and the `fromSlice` markers are two
 * copies of the same plan, so a change to one that the other does not carry
 * fails the script instead of silently postponing a check.
 */
export function stageTableFailures(): string[] {
  const failures: string[] = [];
  for (const slice of sliceOrder) {
    if (!(slice in stageTable)) failures.push(`the stage table has no entry for ${slice}`);
  }
  const listed = new Map<string, string[]>();
  for (const [slice, ids] of Object.entries(stageTable)) {
    for (const id of ids) listed.set(id, [...(listed.get(id) ?? []), slice]);
  }
  for (const [id, slices] of listed) {
    if (slices.length > 1) failures.push(`assertion ${id} is listed by ${slices.length} stages (${slices.join(", ")})`);
  }
  const declared = new Set(assertions.map((assertion) => assertion.id));
  for (const id of listed.keys()) {
    if (!declared.has(id)) failures.push(`the stage table lists the unknown assertion ${id}`);
  }
  for (const assertion of assertions) {
    const slices = listed.get(assertion.id) ?? [];
    if (slices.length === 1 && slices[0] !== assertion.fromSlice) {
      failures.push(`assertion ${assertion.id} declares fromSlice ${assertion.fromSlice} but the stage table lists it under ${slices[0]}`);
    }
  }
  const active = assertions
    .filter((assertion) => sliceIndex(assertion.fromSlice) <= sliceIndex(currentSlice))
    .map((assertion) => assertion.id).sort();
  const tableActive = Object.entries(stageTable)
    .filter(([slice]) => sliceIndex(slice) <= sliceIndex(currentSlice))
    .flatMap(([, ids]) => ids).sort();
  if (active.join(",") !== tableActive.join(",")) {
    failures.push(`the active assertions (${active.join(",") || "none"}) do not match the stage table up to ${currentSlice} (${tableActive.join(",") || "none"})`);
  }
  return failures;
}

export type VerificationResult = {
  currentSlice: Slice;
  active: string[];
  pending: { id: string; fromSlice: Slice }[];
  failures: { id: string; fromSlice: Slice; description: string; scope: string; findings: Finding[]; detail: string }[];
  metaFailures: string[];
  passed: boolean;
};

export function verifyNoControlPlane(root: string = repositoryRoot): VerificationResult {
  const metaFailures = stageTableFailures();
  const active = assertions.filter((assertion) => sliceIndex(assertion.fromSlice) <= sliceIndex(currentSlice));
  const pending = assertions
    .filter((assertion) => sliceIndex(assertion.fromSlice) > sliceIndex(currentSlice))
    .map((assertion) => ({ id: assertion.id, fromSlice: assertion.fromSlice }));
  const failures: VerificationResult["failures"] = [];
  for (const assertion of active) {
    const outcome = assertion.check(root);
    if (outcome.findings.length === 0) continue;
    failures.push({
      id: assertion.id,
      fromSlice: assertion.fromSlice,
      description: assertion.description,
      scope: assertion.scope,
      findings: outcome.findings,
      detail: outcome.detail
    });
  }
  return {
    currentSlice,
    active: active.map((assertion) => assertion.id),
    pending,
    failures,
    metaFailures,
    passed: metaFailures.length === 0 && failures.length === 0
  };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    if (process.argv.length !== 2) throw new Error("verify-no-control-plane accepts no arguments");
    const result = verifyNoControlPlane();
    // One report, one stream, one order: a reader sees the verdict after the
    // evidence that produced it, whichever stream the caller captures.
    const report = [
      `Control Plane retirement residue gate (current slice ${result.currentSlice})`,
      `active assertions: ${result.active.length}`,
      `pending assertions: ${result.pending.length}`,
      ...result.pending.map((pending) => `pending: ${pending.id} (fromSlice ${pending.fromSlice})`),
      ...result.metaFailures.map((failure) => `FAIL: stage table: ${failure}`),
      ...result.failures.flatMap((failure) => [
        `FAIL: ${failure.id} (fromSlice ${failure.fromSlice}): ${failure.detail}`,
        `  assertion: ${failure.description}`,
        `  scope: ${failure.scope}`,
        ...failure.findings.map((finding) => `  ${finding.file}:${finding.line}: ${finding.text}`)
      ]),
      result.passed
        ? "PASS: no Control Plane residue assertion failed"
        : "FAIL: Control Plane residue found"
    ];
    process.stdout.write(`${report.join("\n")}\n`);
    process.exitCode = result.passed ? 0 : 1;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}

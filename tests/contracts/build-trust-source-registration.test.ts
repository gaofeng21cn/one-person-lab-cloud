import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import YAML from "yaml";

// The Build owner is the producer of the workspace image trust source: it is
// the only owner that knows a repository was published by one of its own jobs.
// The Control Plane owns the trust source and the Fabric owner reads it, so the
// producer is the missing half of that route.
//
// The Build owner's Go tests are compiled but not executed by the CI `go`
// group, so this contract keeps the producer's wiring visible to the CI
// `source` group: what writes an admission intent, what publishes it, what a
// refusal leaves behind, and what the published installation declares.

const workerPath = "services/build/internal/build/worker.go";
const producerPath = "services/build/internal/build/trust_source.go";
const migrationPath = "services/build/migrations/0003_trust_source.sql";
const topologyPath = "deploy/portable/opl-cloud-owner-topology.json";
const envExamplePath = "deploy/portable/opl-cloud.env.example";
const composePath = "deploy/portable/compose.cloud.yaml";

const registrationsTable = "build.build_trust_source_registrations";

async function source(path: string): Promise<string> {
  return readFile(path, "utf8");
}

// functionBody returns the body of one Go function, so an assertion can name
// the function it is about instead of matching any occurrence in the package.
function functionBody(sources: string, signature: string): string {
  const start = sources.indexOf(signature);
  assert.notEqual(start, -1, `the package must declare ${signature}`);
  const next = sources.indexOf("\nfunc ", start + signature.length);
  return next === -1 ? sources.slice(start) : sources.slice(start, next);
}

test("the Build owner records one admission intent with the artifact it published", async () => {
  const worker = await source(workerPath);
  const confirm = functionBody(worker, "func (s *Service) confirm(");

  assert.match(confirm, new RegExp(`INSERT INTO ${registrationsTable}\\(id,build_job_id,release_repository,release_version\\)`),
    "the confirmed artifact must carry its admission intent");
  assert.match(confirm, /"tstr_"\+r\.Job\.Id, r\.Job\.Id, repository, r\.Job\.Id/,
    "the intent must carry the repository the artifact was published to and the build job id as the release version");
  assert.match(confirm, /ON CONFLICT \(build_job_id\) DO NOTHING/,
    "one build job may only hold one admission intent");

  const packageInserts = worker.split(`INSERT INTO ${registrationsTable}`).length - 1;
  assert.equal(packageInserts, 1,
    "only the confirmed artifact path may write an admission intent, so a failed build never registers");
});

test("the admission intent is written in the same transaction as the artifact", async () => {
  const worker = await source(workerPath);
  const confirm = functionBody(worker, "func (s *Service) confirm(");

  const artifact = confirm.indexOf("INSERT INTO build.build_artifacts");
  const intent = confirm.indexOf(`INSERT INTO ${registrationsTable}`);
  const commit = confirm.indexOf("tx.Commit()");
  assert.ok(artifact !== -1 && intent !== -1 && commit !== -1, "the confirmation transaction must write both facts");
  assert.ok(artifact < intent, "the artifact is written before the intent that names it");
  assert.ok(intent < commit, "the intent is committed with the artifact, never after it");
});

test("the producer publishes the admission from the worker loop, not from a request path", async () => {
  const worker = await source(workerPath);
  const producer = await source(producerPath);

  assert.match(worker, /return s\.registerTrustSources\(ctx\)/,
    "the worker loop must publish the admission intents it owes");
  assert.match(producer, /func registerPendingTrustSources\(ctx context\.Context, db \*sql\.DB, registrar TrustSourceRegistrar\) error/,
    "the publication must read this owner's own intents and write them through the Control Plane");
  assert.match(producer, /WHERE r\.status<>'registered' AND r\.next_attempt_at<=now\(\)/,
    "the producer must publish the intents that are still open and due");
});

test("the admission is published in the step that confirms the artifact", async () => {
  const worker = await source(workerPath);
  const producer = await source(producerPath);

  assert.match(worker, /func \(s \*Service\) confirmAndAdmit\(/,
    "the worker must confirm the artifact and admit it in one step");
  assert.match(worker, /if err := s\.confirm\(ctx, r, repository, m\); err != nil \{\s*return err\s*\}\s*s\.publishTrustSource\(ctx, r\.Job\.Id\)/,
    "the admission must be published right after the artifact is confirmed, not on a later tick");
  assert.match(worker, /return s\.confirmAndAdmit\(/,
    "the worker must confirm and admit in one step wherever an artifact is confirmed");
  assert.match(producer, /func \(s \*Service\) publishTrustSource\(ctx context\.Context, buildJobID string\)/,
    "the producer must publish one build job's own admission intent");
  assert.match(producer, /WHERE r\.build_job_id=\$1 AND r\.status<>'registered'/,
    "the confirmation step must publish the intent of its own build job");
});

test("the published admission carries the revision the producer read", async () => {
  const producer = await source(producerPath);

  assert.match(producer, /ExpectedRevision:\s+source\.Revision/,
    "the producer must state the trust source revision it read");
  assert.match(producer, /ReleaseVersion:\s+intent\.ReleaseVersion/,
    "the producer must publish the version identity it recorded");
  assert.match(producer, /ReleaseRepository:\s+intent\.ReleaseRepository/,
    "the producer must publish the repository it published to");
  assert.match(producer, /if source\.TrustsRepository\(intent\.ReleaseRepository\)/,
    "a repository that is already admitted must not be admitted again");
});

test("a refused Control Plane leaves the build job carrying the open item", async () => {
  const producer = await source(producerPath);

  assert.match(producer, /trustSourceRegistrationFailed = "trust_source_registration_failed"/,
    "the open item must have one code the build job carries");
  assert.match(producer, /UPDATE build\.build_jobs SET error_code=\$2[^\n]*trustSourceRegistrationFailed/,
    "the build job must carry the open item while its admission is open");
  assert.match(producer, /UPDATE build\.build_jobs SET error_code=NULL[^\n]*trustSourceRegistrationFailed/,
    "the open item must be cleared once the admission is confirmed");
  assert.match(producer, /last_error_code=\$3,next_attempt_at=now\(\)\+\(\$4::double precision\*interval '1 second'\)/,
    "a refused admission must keep its intent open with a landing point to retry from");
  assert.match(producer, /INSERT INTO build\.build_logs[^\n]*'error'/,
    "a refused admission must be logged on the build job that produced it");
});

test("the producer authenticates with the shared credential the Control Plane already holds", async () => {
  const producer = await source(producerPath);

  assert.match(producer, /getenv\("OPL_CONTROL_PLANE_URL"\)/,
    "the producer must read the Control Plane address the installation declares");
  assert.match(producer, /getenv\("OPL_FABRIC_CAPABILITY_KEY"\)/,
    "the producer must read the shared credential the Control Plane already holds");
  assert.match(producer, /contracts\.WorkspaceImageTrustSourceTokenHeader, "Bearer "\+c\.token/,
    "the producer must present the credential the way the Control Plane reads it");
  assert.match(producer, /contracts\.WorkspaceImageTrustSourcePath/,
    "the producer must use the route the shared contract names");
});

test("the migration gives every build job exactly one admission intent", async () => {
  const migration = await source(migrationPath);
  const worker = await source(workerPath);

  assert.match(migration, new RegExp(`CREATE TABLE ${registrationsTable}`),
    "the migration must create the table the producer writes");
  assert.match(migration, /UNIQUE \(build_job_id\)/,
    "one build job may only hold one admission intent");
  assert.match(migration, /GRANT SELECT, INSERT, UPDATE, DELETE ON build\.build_trust_source_registrations TO opl_build_writer/,
    "the owner's writer role must be able to publish and close an admission intent");
  assert.ok(worker.includes(registrationsTable),
    "the producer must write the table the migration creates");
});

test("the published owner topology declares the facts the producer reads", async () => {
  const topology = JSON.parse(await source(topologyPath));
  const build = topology.processes.find((process: { service: string }) => process.service === "build");

  assert.ok(build, "the published owner topology must declare the Build owner");
  const declared = [...(build.env ?? []), ...(build.optionalEnv ?? []), ...(build.externalEnv ?? [])];
  for (const name of ["OPL_CONTROL_PLANE_URL", "OPL_FABRIC_CAPABILITY_KEY"]) {
    assert.ok(declared.includes(name),
      `the published owner topology must declare ${name} for the Build owner`);
  }
});

test("the published environment template names the facts the producer reads", async () => {
  const template = await source(envExamplePath);

  for (const name of ["OPL_CONTROL_PLANE_URL", "OPL_FABRIC_CAPABILITY_KEY"]) {
    assert.match(template, new RegExp(`^${name}=`, "m"),
      `the published environment template must name ${name}`);
  }
});

test("the published installation hands the producer the Control Plane and the shared credential", async () => {
  const compose = YAML.parse(await source(composePath));
  const build = compose.services.build.environment;

  assert.match(String(build.OPL_CONTROL_PLANE_URL), /^\$\{OPL_CONTROL_PLANE_URL:-http:\/\/control-plane:8787\}$/,
    "the Build owner must reach the Control Plane the installation names, with the topology default as the fallback");
  assert.match(String(build.OPL_FABRIC_CAPABILITY_KEY), /^\$\{OPL_FABRIC_CAPABILITY_KEY/,
    "the shared credential must come from the installation, not from a value the compose hardcodes");
});

// The producer has to be generic. The registration names the repository this
// build published, so the code that publishes it may not carry one
// installation's repository, image, host or digest: a literal like that would
// make the trigger work for that installation only.
const producerFiles = [
  "services/build/internal/build/trust_source.go",
  "services/build/internal/build/worker.go",
  "services/build/internal/build/service.go",
  "services/build/cmd/server/main.go",
  "services/build/migrations/0003_trust_source.sql",
];

const instanceFacts: { pattern: RegExp; what: string }[] = [
  { pattern: /[a-z0-9][a-z0-9.-]*\.[a-z]{2,}(?::\d+)?\/[a-z0-9._/-]+/i, what: "a repository or image reference" },
  { pattern: /(?:\d{1,3}\.){3}\d{1,3}/, what: "an IP address" },
  { pattern: /[a-z0-9.-]+:\d{2,5}\/[a-z0-9._/-]+/i, what: "a host:port repository reference" },
  { pattern: /["'][^"']*[0-9a-f]{16,}[^"']*["']/, what: "a pinned digest or identifier" },
];

// literals returns the string literals of a Go or SQL file without the import
// paths: a module path is not an instance fact.
function literals(contents: string): string[] {
  const withoutImports = contents.replace(/import\s*\([^)]*\)/gs, "").replace(/import\s+"[^"]*"/g, "");
  return [
    ...[...withoutImports.matchAll(/"(?:[^"\\]|\\.)*"/g)].map((match) => match[0]),
    ...[...withoutImports.matchAll(/`(?:[^`\\]|\\.)*`/g)].map((match) => match[0]),
  ];
}

test("the producer carries no instance fact of its own", async () => {
  for (const path of producerFiles) {
    const contents = await source(path);
    for (const literal of literals(contents)) {
      for (const { pattern, what } of instanceFacts) {
        assert.doesNotMatch(literal, pattern, `${path} must not carry ${what}: ${literal}`);
      }
    }
  }
});

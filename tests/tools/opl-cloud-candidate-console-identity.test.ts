import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// The shipped cloud product must bake the cloud Console identity, not the
// legacy default. The identity selects the customer "智能体" (/console/agents)
// and Publisher surfaces at build time, so a Candidate built from the wrong
// identity installs a Console where the customer cannot see or use Agents —
// the exact defect that made the deployed Console miss "智能体".
//
// The Dockerfile keeps its neutral legacy default (owner-topology declares that
// as the image's default identity); the Candidate — the shipped cloud product —
// must select cloud explicitly and prove it on the emitted OCI.

test("the Candidate workflow builds and reads back the cloud Console identity", async () => {
  const workflow = await readFile(".github/workflows/build-opl-cloud-candidate.yml", "utf8");
  assert.ok(
    workflow.includes("--build-arg VITE_CONSOLE_IDENTITY=cloud"),
    "the Candidate build must pass VITE_CONSOLE_IDENTITY=cloud explicitly, not rely on the Dockerfile default"
  );
  assert.ok(
    workflow.includes('opl.cloud.console-identity"] == "cloud'),
    "the Candidate readback must prove the emitted image carries the cloud console identity label"
  );
});

test("the image keeps the neutral default while labelling its identity", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const declarations = [...dockerfile.matchAll(/ARG VITE_CONSOLE_IDENTITY=(\S+)/g)].map((m) => m[1]);
  assert.equal(declarations.length, 2, "VITE_CONSOLE_IDENTITY must be declared in the build and runtime stages");
  assert.deepEqual(new Set(declarations), new Set(["legacy"]), "the image default stays the neutral legacy identity");
  assert.match(
    dockerfile,
    /^LABEL opl\.cloud\.console-identity=\$VITE_CONSOLE_IDENTITY$/m,
    "the runtime image must label opl.cloud.console-identity so a digest's surface is readable"
  );
});

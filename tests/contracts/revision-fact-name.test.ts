import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

// The product revision is one fact. It reached the product under three names,
// and every installation had to know which one the topology it used read. The
// topology contract already declares the one name an installation provides, so
// that name is the only one the product may read, and the environment
// template may declare it exactly once.

const topology = JSON.parse(await readFile("deploy/portable/opl-cloud-owner-topology.json", "utf8"));

function declaredEnvNames() {
  const names = new Set();
  for (const name of topology.sharedEnv?.hostProvided || []) names.add(name);
  for (const process of topology.processes || []) {
    for (const name of process.env || []) names.add(name);
  }
  return names;
}

function revisionLikeNames(text) {
  // The environment template assigns with `=` and a Compose file maps with `:`,
  // so both spellings name the same fact and both have to be checked.
  return [...new Set([...text.matchAll(/^\s*(OPL_[A-Z0-9_]*(?:RELEASE_SHA|SOURCE_SHA|PRODUCT_SHA))\s*[=:]/gmu)]
    .map((match) => match[1]))];
}

test("the topology declares exactly one product revision variable", () => {
  const revisions = [...declaredEnvNames()].filter((name) => /(?:RELEASE_SHA|SOURCE_SHA|PRODUCT_SHA)$/.test(name));
  assert.deepEqual(revisions, ["OPL_RELEASE_SHA"], "the installation provides the product revision under one name");
});

test("the environment template declares the product revision exactly once", async () => {
  const template = await readFile("deploy/portable/opl-cloud.env.example", "utf8");
  const revisions = revisionLikeNames(template);
  assert.deepEqual(revisions, ["OPL_RELEASE_SHA"], "the environment template declares the product revision once");
});

test("no portable deployment reads the product revision under another name", async () => {
  const files = [
    ...(await readdir("deploy/portable")).filter((name) => name.endsWith(".yaml")).map((name) => `deploy/portable/${name}`),
    "compose.yaml",
  ];
  for (const file of files) {
    const source = await readFile(file, "utf8");
    for (const name of revisionLikeNames(source)) {
      assert.equal(name, "OPL_RELEASE_SHA", `${file} reads the product revision as ${name}`);
    }
  }
});

test("the product revision reaches the product under one name in the portable tooling", async () => {
  for (const file of ["tools/local-v2-clean-host.ts", "tools/local-workspace-qualification.ts"]) {
    const source = await readFile(file, "utf8");
    for (const name of revisionLikeNames(source)) {
      assert.equal(name, "OPL_RELEASE_SHA", `${file} provides the product revision as ${name}`);
    }
  }
});

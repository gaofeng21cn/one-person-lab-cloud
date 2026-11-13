import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";
import { parse } from "yaml";
import {
  localV2ReferencedVariables, localV2TemplateDeclarations,
} from "../../tools/local-v2-installation-facts.ts";

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

test("the topology declares exactly one product revision variable", () => {
  const revisions = [...declaredEnvNames()].filter((name) => /(?:RELEASE_SHA|SOURCE_SHA|PRODUCT_SHA)$/.test(name));
  assert.deepEqual(revisions, ["OPL_RELEASE_SHA"], "the installation provides the product revision under one name");
});

test("the environment template declares the product revision exactly once", async () => {
  const template = await readFile("deploy/portable/opl-cloud.env.example", "utf8");
  const { declarations, duplicates } = localV2TemplateDeclarations(template);
  assert.deepEqual(duplicates, [], "the environment template must not repeat a declaration");
  const revisions = declarations.map(({ name }) => name)
    .filter((name) => /(?:RELEASE_SHA|SOURCE_SHA|PRODUCT_SHA)$/.test(name));
  assert.deepEqual(revisions, ["OPL_RELEASE_SHA"], "the environment template declares the product revision once");
});

test("no portable deployment reads the product revision under another name", async () => {
  const files = [
    ...(await readdir("deploy/portable")).filter((name) => name.endsWith(".yaml")).map((name) => `deploy/portable/${name}`),
    "compose.yaml",
  ];
  let revisionInputs = 0;
  for (const file of files) {
    const source = await readFile(file, "utf8");
    for (const name of localV2ReferencedVariables(source)
      .filter((name) => /(?:RELEASE_SHA|SOURCE_SHA|PRODUCT_SHA)$/.test(name))) {
      assert.equal(name, "OPL_RELEASE_SHA", `${file} reads the product revision as ${name}`);
    }
    const compose = parse(source, { merge: true });
    for (const [service, config] of Object.entries(compose.services as Record<string, {
      environment?: Record<string, unknown> | string[];
    }>)) {
      const environment = config.environment ?? {};
      const entries = Array.isArray(environment)
        ? environment.map((entry) => {
          const offset = entry.indexOf("=");
          return offset < 0 ? [entry, null] : [entry.slice(0, offset), entry.slice(offset + 1)];
        })
        : Object.entries(environment);
      for (const [name, value] of entries) {
        if (name !== "OPL_RELEASE_SHA") continue;
        revisionInputs += 1;
        assert.deepEqual(localV2ReferencedVariables(value), ["OPL_RELEASE_SHA"],
          `${file} ${service}.OPL_RELEASE_SHA must consume the one installation revision variable`);
      }
    }
  }
  assert.ok(revisionInputs > 0, "the Compose deployments must consume the product revision");
});

test("no installation fact carries the topology's name", async () => {
  // "local-v2" names the topology, not the installation. A fact that carries
  // it tells an operator that this installation has a second kind, and makes
  // the qualification stack and the published stack speak two vocabularies
  // for one fact. Whoever runs an installation sets the same name; the values
  // are what differ.
  const files = [
    "deploy/portable/opl-cloud.env.example",
    "compose.yaml",
    ...(await readdir("deploy/portable")).filter((name) => name.endsWith(".yaml")).map((name) => `deploy/portable/${name}`),
    "tools/local-v2-clean-host.ts",
    "tools/local-workspace-qualification.ts"
  ];
  for (const file of files) {
    const source = await readFile(file, "utf8");
    const names = [...new Set([...source.matchAll(/\bOPL_LOCAL_V2_[A-Z0-9_]+/g)].map((match) => match[0]))];
    assert.deepEqual(names, [], `${file} carries the topology's name in an installation fact`);
  }
});

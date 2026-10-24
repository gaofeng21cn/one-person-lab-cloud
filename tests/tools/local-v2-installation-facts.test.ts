import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  LOCAL_V2_MINIMUM_TOKEN_LENGTH,
  localV2EnvFile,
  localV2FactsError,
  localV2InstallationFacts,
  localV2ReferencedVariables,
  localV2TargetTokenVariable,
} from "../../tools/local-v2-installation-facts.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

const template = await readFile(resolve(root, "deploy/portable/opl-cloud.env.example"), "utf8");
const compose = await readFile(resolve(root, "deploy/portable/compose.local-v2.yaml"), "utf8");

// A distinct, long value per name. Distinctness matters: two database roles
// that share one password are refused, and the refusal is the point.
function suppliedValue(name) {
  return createHash("sha256").update(`local-v2-test:${name}`).digest("hex");
}

// Every variable the topology reads that the template still leaves as a
// placeholder has to be supplied by the installation.
function suppliedValues() {
  const values = {};
  for (const match of template.matchAll(/^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/gm)) {
    if (!match[2].includes("<")) continue;
    if (match[1].endsWith("_PEER_TOKENS")) continue;
    if (!localV2ReferencedVariables(compose).includes(match[1])) continue;
    values[match[1]] = suppliedValue(match[1]);
  }
  return values;
}

test("the topology reads only variables the template declares", () => {
  const referenced = localV2ReferencedVariables(compose);
  assert.ok(referenced.length >= 70, `expected the topology to read many variables, saw ${referenced.length}`);
  const declared = new Set([...template.matchAll(/^([A-Za-z_][A-Za-z0-9_]*)=/gm)].map((match) => match[1]));
  const missing = referenced.filter((name) => !declared.has(name));
  assert.deepEqual(missing, [], "the topology must not read a variable the template does not declare");
});

test("one installation derives its environment from the template alone", () => {
  const facts = localV2InstallationFacts({ template, compose, values: suppliedValues() });
  const declared = new Set(facts.entries.map(([name]) => name));
  for (const name of facts.referenced) assert.ok(declared.has(name), `${name} must be in the derived environment`);
  for (const [, value] of facts.entries) {
    assert.ok(!(value.includes("<") && value.includes(">")), "no raw placeholder may survive");
  }
});

test("a target's allowlist carries that target's one token for every caller", () => {
  const values = suppliedValues();
  const facts = localV2InstallationFacts({ template, compose, values });
  const allowlist = (name) => JSON.parse(facts.values[name]);
  const tenant = allowlist("OPL_TENANT_PEER_TOKENS");
  assert.ok(Object.keys(tenant).length >= 8);
  for (const peer of Object.keys(tenant)) {
    assert.equal(tenant[peer], values.OPL_CLOUD_IDENTITY_TOKEN, `the tenant admits ${peer} with the CloudIdentity token`);
  }
  const capability = allowlist("OPL_CAPABILITY_PEER_TOKENS");
  for (const peer of Object.keys(capability)) {
    assert.equal(capability[peer], values.OPL_CAPABILITY_TOKEN, `the capability admits ${peer} with its own token`);
  }
  const fabric = allowlist("OPL_FABRIC_PEER_TOKENS");
  for (const peer of Object.keys(fabric)) {
    assert.equal(fabric[peer], values.OPL_FABRIC_TOKEN);
  }
  assert.ok(Object.values(tenant).every((token) => token.length >= LOCAL_V2_MINIMUM_TOKEN_LENGTH));
});

test("the tenant deployment unit is reached with the CloudIdentity token", () => {
  assert.equal(localV2TargetTokenVariable("OPL_TENANT_PEER_TOKENS"), "OPL_CLOUD_IDENTITY_TOKEN");
  assert.equal(localV2TargetTokenVariable("OPL_RESOURCE_CATALOG_PEER_TOKENS"), "OPL_RESOURCE_CATALOG_TOKEN");
});

test("a variable the topology reads that the installation did not supply is refused", () => {
  const values = suppliedValues();
  delete values.OPL_CLOUD_IMAGE;
  assert.throws(() => localV2InstallationFacts({ template, compose, values }),
    (error) => error.code === "local_v2_variable_unfilled" && error.message.includes("OPL_CLOUD_IMAGE"));
});

test("required values reject newline injection and empty placeholders", () => {
  const newline = suppliedValues();
  newline.OPL_CLOUD_IMAGE = "image\nOPL_WORKSPACE_IMAGE=spoofed";
  assert.throws(() => localV2InstallationFacts({ template, compose, values: newline }),
    (error) => error.code === "local_v2_value_contains_newline");

  const empty = suppliedValues();
  empty.OPL_CLOUD_IMAGE = "";
  assert.throws(() => localV2InstallationFacts({ template, compose, values: empty }),
    (error) => error.code === "local_v2_required_value_empty" && error.message.includes("OPL_CLOUD_IMAGE"));
});

test("sentinel detection does not reject a legitimate path containing unset", () => {
  const values = suppliedValues();
  values.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT = "/srv/unset-workspaces";
  const facts = localV2InstallationFacts({ template, compose, values });
  assert.equal(facts.values.OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT, "/srv/unset-workspaces");
});

test("a variable the topology reads that the template does not declare is refused", () => {
  const values = suppliedValues();
  const shortTemplate = template.split("\n").filter((line) => !line.startsWith("OPL_CLOUD_IMAGE=")).join("\n");
  assert.throws(() => localV2InstallationFacts({ template: shortTemplate, compose, values }),
    (error) => error.code === "local_v2_variable_not_declared" && error.message.includes("OPL_CLOUD_IMAGE"));
});

test("a template that declares one name twice is refused when the topology reads it", () => {
  const doubled = `${template}\nOPL_CLOUD_IMAGE=other\n`;
  assert.throws(() => localV2InstallationFacts({ template: doubled, compose, values: suppliedValues() }),
    (error) => error.code === "local_v2_variable_declared_twice" && error.message.includes("OPL_CLOUD_IMAGE"));
});

test("a template that declares one name twice is reported when the topology does not read it", () => {
  const facts = localV2InstallationFacts({ template, compose, values: suppliedValues() });
  assert.ok(facts.duplicates.length >= 1, "the template's upstream duplicates are reported, not hidden");
  for (const duplicate of facts.duplicates) {
    assert.ok(!facts.referenced.includes(duplicate.name),
      `${duplicate.name} is declared twice, so the topology must not read it`);
  }
  const declaredOnce = facts.entries.map(([name]) => name);
  assert.equal(new Set(declaredOnce).size, declaredOnce.length);
});

test("two database roles that share one password are refused", () => {
  const values = suppliedValues();
  values.OPL_LEDGER_DATABASE_PASSWORD = values.OPL_BUILD_DATABASE_PASSWORD;
  assert.throws(() => localV2InstallationFacts({ template, compose, values }),
    (error) => error.code === "local_v2_password_shared");
});

test("a database password shorter than the initialization refuses is refused", () => {
  const values = suppliedValues();
  values.OPL_LEDGER_DATABASE_PASSWORD = "short";
  assert.throws(() => localV2InstallationFacts({ template, compose, values }),
    (error) => error.code === "local_v2_password_too_short");
});

test("a target token shorter than the transport contract accepts is refused", () => {
  const values = suppliedValues();
  values.OPL_BUILD_TOKEN = "short";
  assert.throws(() => localV2InstallationFacts({ template, compose, values }),
    (error) => error.code === "local_v2_target_token_missing" && error.message.includes("OPL_BUILD_PEER_TOKENS"));
});

test("the derived environment file declares every name exactly once", () => {
  const facts = localV2InstallationFacts({ template, compose, values: suppliedValues() });
  const file = localV2EnvFile(facts, { header: "derived from the template" });
  const names = file.split("\n").filter((line) => line && !line.startsWith("#")).map((line) => line.split("=")[0]);
  assert.equal(new Set(names).size, names.length, "a name declared twice shadows the earlier value");
  assert.ok(names.includes("OPL_CLOUD_IMAGE"));
});

test("an allowlist that disagrees with the target's token is refused", () => {
  const values = suppliedValues();
  values.OPL_BUILD_PEER_TOKENS = JSON.stringify({ capability: "a-different-token-value-that-is-long-enough" });
  assert.throws(() => localV2InstallationFacts({ template, compose, values }),
    (error) => error.code === "local_v2_peer_tokens_conflict" && error.message.includes("OPL_BUILD_PEER_TOKENS"));
});

test("the facts error carries a code a caller can branch on", () => {
  const error = localV2FactsError("local_v2_example", "detail");
  assert.equal(error.code, "local_v2_example");
  assert.match(error.message, /local_v2_example: detail/);
});

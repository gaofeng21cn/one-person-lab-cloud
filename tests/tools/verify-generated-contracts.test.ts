import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import type { TestContext } from "node:test";
import {
  generationInputs,
  generatedOutputs,
  protoDescriptorProgram,
  verifyGeneratedContracts,
  verifyProtoSchema
} from "../../tools/verify-generated-contracts.ts";
import type { GenerationCommand, GenerationRunner } from "../../tools/verify-generated-contracts.ts";

const proto = "packages/contracts/proto";
const target = "docs/spec/target";

function put(root: string, path: string, data: string | Buffer) {
  mkdirSync(dirname(join(root, path)), { recursive: true });
  writeFileSync(join(root, path), data);
}

// A deterministic dependency-aware runner, not an assertion/hash manifest.
// Real generator execution runs through `npm run verify:generated-contracts`; it
// is not yet a blocking CI required check until the contracts owner reconciles the
// production/target proto drift.
function fakeGeneration({ args, cwd }: GenerationCommand) {
  const contents = (...paths: string[]) => Buffer.concat(paths.map((path) => readFileSync(join(cwd, path))));
  if (args[0] === "-c") {
    assert.equal(args[1], protoDescriptorProgram);
    // Unit tests inject descriptor bytes; opt-in tests below exercise real protoc.
    put(cwd, args[4], contents(args[2]));
    put(cwd, args[5], contents(args[3]));
    put(cwd, args[6], "fixture descriptor source difference");
  } else if (args[0] === `${proto}/generate.sh`) {
    for (const output of generatedOutputs.slice(0, 2)) {
      put(cwd, output, contents(`${proto}/generate.sh`, `${proto}/internal.proto`));
    }
    put(cwd, generatedOutputs[2], contents(
      `${proto}/generate_public_json_shape.py`, `${target}/contracts/internal.proto`,
      `${target}/03_api_contract_complete.yaml`, `${target}/contracts/publisher-contract.schema.json`
    ));
  } else {
    assert.deepEqual(args, [`${proto}/generate_event_identity.py`, `${proto}/events.json`, generatedOutputs[3]]);
    put(cwd, generatedOutputs[3], contents(`${proto}/generate_event_identity.py`, `${proto}/events.json`));
  }
}

function fixture(t: TestContext) {
  const root = mkdtempSync(join(tmpdir(), "opl-freshness-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const path of generationInputs) put(root, path, `input: ${path}\n`);
  put(root, `${proto}/events.json`, JSON.stringify({ oneOf: [], description: "event contract" }));
  for (const name of ["internal.proto", "events.json"]) {
    put(root, `${target}/contracts/${name}`, readFileSync(join(root, `${proto}/${name}`)));
  }
  fakeGeneration({ command: "bash", args: [`${proto}/generate.sh`], cwd: root });
  fakeGeneration({ command: "python3", args: [`${proto}/generate_event_identity.py`, `${proto}/events.json`, generatedOutputs[3]], cwd: root });
  return root;
}

test("freshness passes by regenerating isolated inputs into empty outputs without rewriting the repository", (t) => {
  const root = fixture(t);
  const originals = new Map([...generationInputs, ...generatedOutputs].map((path) => [path, readFileSync(join(root, path))]));
  const calls: GenerationCommand[] = [];
  const runner: GenerationRunner = (command) => {
    assert.notEqual(command.cwd, root);
    if (calls.length === 0) {
      for (const output of generatedOutputs) assert.equal(existsSync(join(command.cwd, output)), false);
      for (const input of generationInputs) assert.deepEqual(readFileSync(join(command.cwd, input)), originals.get(input));
    }
    calls.push(command);
    fakeGeneration(command);
  };
  const result = verifyGeneratedContracts({ root, runner });
  assert.equal(result.passed, true);
  assert.deepEqual(result.errors, []);
  assert.deepEqual(result.compared, [...generatedOutputs].sort());
  assert.match(result.uncovered.join("\n"), /events.json.*NOT regenerated/);
  assert.equal(calls.length, 3);
  assert.equal(calls[0].command, process.env.PYTHON || "python3");
  assert.equal(calls[1].command, "bash");
  assert.equal(calls[2].command, process.env.PYTHON || "python3");
  for (const call of calls) assert.equal(existsSync(call.cwd), false, "temporary workspace leaked");
  for (const [path, bytes] of originals) assert.deepEqual(readFileSync(join(root, path)), bytes);
});

for (const [name, inputs, outputs] of [
  ["both proto inputs", [`${proto}/internal.proto`, `${target}/contracts/internal.proto`], generatedOutputs.slice(0, 3)],
  ["API schema", [`${target}/03_api_contract_complete.yaml`], [generatedOutputs[2]]],
  ["publisher schema", [`${target}/contracts/publisher-contract.schema.json`], [generatedOutputs[2]]],
  ["event identities input", [`${proto}/events.json`, `${target}/contracts/events.json`], [generatedOutputs[3]]],
  ["generator source", [`${proto}/generate_public_json_shape.py`], [generatedOutputs[2]]]
] as const) {
  test(`freshness rejects ${name} drift even when duplicate sources still agree`, (t) => {
    const root = fixture(t);
    for (const path of inputs) put(root, path, JSON.stringify("changed input"));
    const result = verifyGeneratedContracts({ root, runner: fakeGeneration });
    assert.equal(result.passed, false);
    assert.ok(result.errors.every((error) => !error.startsWith("proto schema differs") && !error.startsWith("source JSON differs")));
    for (const output of outputs) assert.ok(result.errors.includes(`generated bytes differ: ${output}`));
  });
}

for (const name of ["internal.proto", "events.json"]) {
  for (const side of [proto, `${target}/contracts`]) {
    test(`freshness detects ${side}/${name} disagreement without replacing either source`, (t) => {
      const root = fixture(t);
      put(root, `${side}/${name}`, JSON.stringify("one-sided drift"));
      let calls = 0;
      const result = verifyGeneratedContracts({ root, runner: (command) => { calls++; fakeGeneration(command); } });
      assert.equal(result.passed, false);
      assert.ok(result.errors.some((error) => error.startsWith(name === "internal.proto" ? "proto schema differs:" : "source JSON differs:")));
      assert.equal(calls, 3, "source conflict must not hide real regeneration evidence");
      assert.equal(readFileSync(join(root, `${side}/${name}`), "utf8"), JSON.stringify("one-sided drift"));
    });
  }
}

test("event source parity ignores formatting and object key order without overwriting either copy", (t) => {
  const root = fixture(t);
  const reformatted = '{\n  "description": "event contract",\n  "oneOf": []\n}\n';
  put(root, `${target}/contracts/events.json`, reformatted);
  const result = verifyGeneratedContracts({ root, runner: fakeGeneration });
  assert.equal(result.passed, true, result.errors.join("\n"));
  assert.equal(readFileSync(join(root, `${target}/contracts/events.json`), "utf8"), reformatted);
});

test("event source parity preserves description conflicts and array order", (t) => {
  const root = fixture(t);
  put(root, `${target}/contracts/events.json`, JSON.stringify({ oneOf: [], description: "conflicting owner claim" }));
  assert.ok(verifyGeneratedContracts({ root, runner: fakeGeneration }).errors.some((error) => error.startsWith("source JSON differs:")));
  put(root, `${proto}/events.json`, JSON.stringify({ oneOf: ["first", "second"] }));
  put(root, `${target}/contracts/events.json`, JSON.stringify({ oneOf: ["second", "first"] }));
  assert.ok(verifyGeneratedContracts({ root, runner: fakeGeneration }).errors.some((error) => error.startsWith("source JSON differs:")));
});

test("descriptor compilation failure cannot silently pass source parity or hide output comparisons", (t) => {
  const root = fixture(t);
  const result = verifyGeneratedContracts({ root, runner: (command) => {
    if (command.args[0] === "-c") throw new Error("grpcio-tools version mismatch: expected 1.80.0, got 0.0.0");
    fakeGeneration(command);
  } });
  assert.equal(result.passed, false);
  assert.ok(result.errors.some((error) => error.includes("grpcio-tools version mismatch")));
  assert.equal(result.compared.length, 4);
});

// Explicit integration mode keeps the ordinary Node source lane dependency-free.
// Run OPL_TEST_PROTO_DESCRIPTORS=1 node --test <this file> with locked grpcio-tools.
if (process.env.OPL_TEST_PROTO_DESCRIPTORS === "1") {
  const production = `syntax = "proto3";
package opl.cloud.api;
option go_package = "opl-cloud/packages/contracts/go/api;api";
option java_package = "opl.cloud.api.literal";
// Production comments may differ from the specification.
message Envelope {
  message Nested { string value = 1; }
  enum Kind { KIND_UNSPECIFIED = 0; KIND_VALUE = 1; }
  oneof body { string text = 1; .opl.cloud.api.Envelope.Nested nested = 2; }
  Kind kind = 3;
  repeated .opl.cloud.api.Envelope.Nested children = 4;
  optional int64 counter = 5;
  reserved 6;
}
service Example { rpc Call(.opl.cloud.api.Envelope) returns (.opl.cloud.api.Envelope); }
`;
  const specification = production
    .replaceAll("opl.cloud.api", "opl.cloud.v226")
    .replace("opl-cloud/packages/contracts/go/api;api", "opl.cloud/contracts/v226;v226")
    .replace('java_package = "opl.cloud.v226.literal"', 'java_package = "opl.cloud.api.literal"')
    .replace("Production comments may differ from the specification.", "Different target comment.");

  const cases: [string, (source: string) => string][] = [
    ["field number", (s) => s.replace("string text = 1", "string text = 10")],
    ["field name", (s) => s.replace("string text", "string renamed")],
    ["scalar type", (s) => s.replace("string text", "bytes text")],
    ["message type", (s) => s.replace("Envelope.Nested nested", "Envelope nested")],
    ["field addition", (s) => s.replace("reserved 6;", "reserved 6; string added = 7;")],
    ["field removal", (s) => s.replace("Kind kind = 3;", "")],
    ["field cardinality", (s) => s.replace("repeated .opl.cloud.v226", ".opl.cloud.v226")],
    ["proto3 optional", (s) => s.replace("optional int64", "int64")],
    ["field options", (s) => s.replace("string text = 1;", "string text = 1 [deprecated = true];")],
    ["oneof name", (s) => s.replace("oneof body", "oneof renamed")],
    ["oneof membership", (s) => s.replace("string text = 1;", "").replace("Kind kind = 3;", "Kind kind = 3; string text = 1;")],
    ["enum value", (s) => s.replace("KIND_VALUE = 1", "KIND_VALUE = 2")],
    ["message addition", (s) => s + "message Added {}\n"],
    ["RPC addition", (s) => s.replace("service Example {", "service Example { rpc Added(Envelope) returns (Envelope);")],
    ["RPC removal", (s) => s.replace(/rpc Call\([^;]+;/, "")],
    ["RPC input", (s) => s.replace("Call(.opl.cloud.v226.Envelope)", "Call(.opl.cloud.v226.Envelope.Nested)")],
    ["RPC output", (s) => s.replace("returns (.opl.cloud.v226.Envelope)", "returns (.opl.cloud.v226.Envelope.Nested)")],
    ["RPC streaming", (s) => s.replace("Call(.opl", "Call(stream .opl")],
    ["undeclared package", (s) => s.replaceAll("opl.cloud.v226", "opl.cloud.future")],
    ["undeclared Go deployment mapping", (s) => s.replace("opl.cloud/contracts/v226;v226", "other/contracts;other")],
    ["unrelated string option", (s) => s.replace('java_package = "opl.cloud.api.literal"', 'java_package = "opl.cloud.v226.literal"')]
  ];

  test("real locked descriptors accept only declared deployment mappings and ignore comments/whitespace", (t) => {
    const root = fixture(t);
    put(root, `${proto}/internal.proto`, production);
    put(root, `${target}/contracts/internal.proto`, specification.replaceAll("\n", "\n\n"));
    assert.doesNotThrow(() => verifyProtoSchema(root));
  });

  for (const [name, mutate] of cases) {
    test(`real locked descriptors reject ${name} drift`, (t) => {
      const root = fixture(t);
      put(root, `${proto}/internal.proto`, production);
      put(root, `${target}/contracts/internal.proto`, mutate(specification));
      assert.throws(() => verifyProtoSchema(root), (error: Error) => {
        assert.match(error.message, /proto schema differs:/);
        assert.match(error.message, /descriptor\.file\[internal\.proto\]/);
        return true;
      });
    });
  }
}

for (const output of generatedOutputs) {
  test(`freshness rejects byte tampering in ${output}`, (t) => {
    const root = fixture(t);
    // Same length, different byte: this must be content comparison, not size/mtime.
    const bytes = readFileSync(join(root, output));
    bytes[0] ^= 1;
    put(root, output, bytes);
    const result = verifyGeneratedContracts({ root, runner: fakeGeneration });
    assert.equal(result.passed, false);
    assert.ok(result.errors.includes(`generated bytes differ: ${output}`));
    assert.deepEqual(readFileSync(join(root, output)), bytes, "verification repaired a tampered output");
  });
}

test("freshness rejects no-op generators instead of comparing seeded generated files", (t) => {
  const result = verifyGeneratedContracts({ root: fixture(t), runner: () => {} });
  assert.equal(result.passed, false);
  assert.equal(result.compared.length, 0);
  for (const output of generatedOutputs) assert.ok(result.errors.some((error) => error.includes(output)));
});

test("freshness checks the full Go binding output set, including obsolete and new files", (t) => {
  const root = fixture(t);
  const obsolete = "packages/contracts/go/api/obsolete.pb.go";
  const added = "packages/contracts/go/api/new.pb.go";
  put(root, obsolete, "obsolete\n");
  const result = verifyGeneratedContracts({ root, runner: (command) => {
    fakeGeneration(command);
    put(command.cwd, added, "new\n");
  } });
  assert.equal(result.passed, false);
  assert.ok(result.errors.includes(`missing regenerated output (or obsolete checked-in binding): ${obsolete}`));
  assert.ok(result.errors.includes(`missing checked-in generated output: ${added}`));
});

test("freshness fails closed on missing tools while still checking the independent event chain", (t) => {
  const root = fixture(t);
  let temporary = "";
  const result = verifyGeneratedContracts({ root, runner: (command) => {
    temporary = command.cwd;
    if (command.command === "bash") throw new Error("missing generator: protoc-gen-go; expected protoc-gen-go v1.36.6");
    fakeGeneration(command);
  } });
  assert.equal(result.passed, false);
  assert.ok(result.errors.some((error) => error.includes("missing generator: protoc-gen-go")));
  assert.ok(result.compared.includes(generatedOutputs[3]));
  assert.equal(existsSync(temporary), false);
});

test("freshness fails on missing inputs and missing checked-in outputs", (t) => {
  const root = fixture(t);
  rmSync(join(root, generatedOutputs[0]));
  assert.ok(verifyGeneratedContracts({ root, runner: fakeGeneration }).errors.includes(
    `missing checked-in generated output: ${generatedOutputs[0]}`
  ));
  rmSync(join(root, generationInputs[0]));
  assert.throws(() => verifyGeneratedContracts({ root, runner: fakeGeneration }), /ENOENT/);
});

// Execute the actual shell preflight with only fake executable tools. No Python
// package, Go installation, network, or regeneration is required for unit tests.
for (const [tool, version, expected] of [
  ["protoc-gen-go", null, /missing generator: protoc-gen-go/],
  ["protoc-gen-go", "protoc-gen-go v0.0.0", /protoc-gen-go version mismatch/],
  ["protoc-gen-go-grpc", null, /missing generator: protoc-gen-go-grpc/],
  ["protoc-gen-go-grpc", "protoc-gen-go-grpc 0.0.0", /protoc-gen-go-grpc version mismatch/],
  ["gofmt", null, /missing formatter: gofmt/],
  ["python3", "libprotoc 0.0", /protoc version mismatch/]
] as const) {
  test(`actual shell preflight rejects ${tool} ${version ?? "missing"}`, (t) => {
    const root = fixture(t);
    const bin = join(root, "bin");
    mkdirSync(bin);
    symlinkSync("/usr/bin/dirname", join(bin, "dirname"));
    const versions: Record<string, string> = {
      "protoc-gen-go": "protoc-gen-go v1.36.6",
      "protoc-gen-go-grpc": "protoc-gen-go-grpc 1.5.1",
      gofmt: "",
      python3: "libprotoc 31.1"
    };
    if (version === null) delete versions[tool];
    else versions[tool] = version;
    for (const [name, output] of Object.entries(versions)) {
      const script = `#!${process.execPath}\nif (process.argv[2] !== '-') process.stdout.write(${JSON.stringify(output + "\n")});\n`;
      put(root, `bin/${name}`, script);
      chmodSync(join(bin, name), 0o755);
    }
    put(root, `${proto}/generate.sh`, readFileSync(new URL(`../../${proto}/generate.sh`, import.meta.url)));
    const result = spawnSync("/bin/bash", [join(root, `${proto}/generate.sh`)], {
      env: { ...process.env, PATH: bin, PYTHON: "python3" }, encoding: "utf8"
    });
    assert.equal(result.status, 1, result.stderr);
    assert.match(result.stderr, expected);
  });
}

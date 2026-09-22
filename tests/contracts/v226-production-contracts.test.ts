import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import test from "node:test";

const protoPath = "packages/contracts/proto/internal.proto";
const specProtoPath = "docs/spec/v2.26/contracts/internal.proto";
const pbPath = "packages/contracts/go/v226/internal.pb.go";
const grpcPath = "packages/contracts/go/v226/internal_grpc.pb.go";

async function text(path: string) {
  return readFile(path, "utf8");
}

test("the production proto is the byte-identical v2.26 specification owner", async () => {
  const [production, spec] = await Promise.all([text(protoPath), text(specProtoPath)]);
  assert.equal(production, spec, "production proto must not diverge from the specification bytes");

  const digest = createHash("sha256").update(production).digest("hex");
  assert.match(digest, /^[0-9a-f]{64}$/);

  // The spec carries the logical go_package; the generator maps it, never edits it.
  assert.match(production, /option go_package = "opl\.cloud\/contracts\/v226;v226";/);
});

test("generated bindings live in the single contracts module and record pinned tools", async () => {
  const [module, pb, grpc] = await Promise.all([
    text("packages/contracts/go/go.mod"),
    text(pbPath),
    text(grpcPath)
  ]);

  assert.match(module, /^module opl-cloud\/packages\/contracts\/go$/m);
  assert.ok(!/^module /m.test(pb) && !/^module /m.test(grpc), "generated files are not modules");

  assert.match(pb, /^package v226$/m);
  assert.match(grpc, /^package v226$/m);

  // Tool versions are pinned so regeneration is deterministic.
  assert.match(pb, /protoc-gen-go v1\.36\.6/);
  assert.match(pb, /protoc\s+v6\.31\.1/);
  assert.match(grpc, /protoc-gen-go-grpc v1\.5\.1/);
  assert.match(module, /google\.golang\.org\/grpc v1\.83\.2/);
});

test("money and duration fields keep integer micros and millisecond precision", async () => {
  const production = await text(protoPath);

  // Money is USD micros on the wire; no float currency types exist.
  assert.ok(production.includes("int64 money=USD micros"), "spec money note retained");
  assert.match(production, /optional int64 monthly_price_usd_micros = 8;/);
  // No scalar float/double field exists; the only "float" token is in a comment.
  assert.equal(/^\s*(optional\s+)?(float|double)\s+\w+/m.test(production), false, "no float/double field");

  // Sub-second time is integer milliseconds, not floating seconds.
  assert.match(production, /int64 period_milliseconds = 1;/);
  assert.match(production, /int64 remaining_milliseconds = 2;/);
});

test("required v2.26 domain services are grouped in one wire spec", async () => {
  const production = await text(protoPath);
  for (const service of [
    "TenantProductService",
    "CapabilityProductService",
    "BuildProductService",
    "ResourceCatalogProductService",
    "WorkspaceProductService",
    "GatewayProductService",
    "LedgerProductService",
    "FabricCoordination"
  ]) {
    assert.match(production, new RegExp(`service ${service} \\{`), `missing service ${service}`);
  }
});

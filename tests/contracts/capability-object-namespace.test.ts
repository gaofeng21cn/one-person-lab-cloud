import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

// The object data plane owns one namespace, /objects. The same-origin entry
// forwards a request's path verbatim, so the signed upload path and the Build
// read path have to be paths the Capability data plane itself serves.
//
// A route registered outside that namespace is shadowed by the read pattern:
// its path matches every /objects/{segment} request and its method is GET, so
// the upload is refused with 405. This test reads the real registrations and
// the real read expression, so the namespace cannot drift apart again.

const capabilityDirectory = "services/capability";
const buildRunner = "services/build/internal/build/runner.go";
const topologyPath = "deploy/portable/opl-cloud-owner-topology.json";
const routeScan = "services/control-plane/internal/server/testdata/http-surface-scan.go";

async function goFiles(directory: string): Promise<string[]> {
  const found: string[] = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) found.push(...(await goFiles(full)));
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) found.push(full);
  }
  return found;
}

// The registered literal paths come from Go's own parser, so a route written
// in any form the parser sees is inventoried and a comment is not.
function registeredLiteralPaths(files: string[]): string[] {
  return JSON.parse(execFileSync("go", ["run", routeScan], {
    input: JSON.stringify(files), encoding: "utf8", maxBuffer: 4 * 1024 * 1024,
  })) as string[];
}

test("the Capability object data plane serves its ingest route inside the object namespace", async () => {
  const registered = registeredLiteralPaths(await goFiles(capabilityDirectory));

  assert.ok(registered.includes("/objects/parts"),
    `Capability must register the signed upload ingest route at /objects/parts, saw ${registered.join(",")}`);
  assert.ok(!registered.includes("/parts"),
    "Capability must not register the ingest route outside the object namespace: the read route's path shadows it and the upload is refused with 405");
  assert.ok(registered.includes("/objects/{digest}"),
    `Capability must keep the object read route at /objects/{digest}, saw ${registered.join(",")}`);
});

test("the Build worker reads the object namespace base without appending the namespace twice", async () => {
  const source = await readFile(buildRunner, "utf8");

  assert.ok(source.includes(`strings.TrimRight(r.StorageURL, "/") + "/"`),
    "the Build worker must read the object at {StorageURL}/{digest}: the storage base already carries the object namespace");
  assert.ok(!source.includes(`+ "/objects/"`),
    "the Build worker must not append the object namespace to a base that already carries it: the read would ask for /objects/objects/{digest}");
});

test("the published owner topology declares the object namespace and no retired ingest prefix", async () => {
  const topology = JSON.parse(await readFile(topologyPath, "utf8"));
  const surfaces = topology.browserApiSurface.surfaces as Array<{ prefix: string; processes: string[] }>;

  const objectNamespace = surfaces.find((surface) => surface.prefix === "/objects/");
  assert.ok(objectNamespace, "the published owner topology must declare the object namespace");
  assert.ok(objectNamespace.processes.includes("capability"),
    "the object namespace belongs to the Capability owner");

  const retired = surfaces.find((surface) => surface.prefix === "/parts/");
  assert.equal(retired, undefined,
    "the published owner topology must not declare a /parts/ prefix: no process registers that route any more");
});

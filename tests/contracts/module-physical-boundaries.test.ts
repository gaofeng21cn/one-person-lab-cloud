import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import test from "node:test";

import { controlPlaneApiPath } from "../../apps/console-ui/src/api/console-api.ts";

const root = new URL("../../", import.meta.url);

async function text(path) {
  return readFile(new URL(path, root), "utf8");
}

async function filesUnder(directory, include) {
  const files = [];
  for (const entry of await readdir(new URL(`${directory}/`, root), { withFileTypes: true })) {
    const path = `${directory}/${entry.name}`;
    if (entry.isDirectory()) files.push(...await filesUnder(path, include));
    else if (entry.isFile() && include(path)) files.push(path);
  }
  return files;
}

function goImports(source) {
  const imports = [...source.matchAll(/^\s*import\s+(?:[._\w]+\s+)?["`]([^"`]+)["`]/gm)].map((match) => match[1]);
  for (const block of source.matchAll(/\bimport\s*\(([\s\S]*?)\)/g)) {
    imports.push(...[...block[1].matchAll(/^\s*(?:[._\w]+\s+)?["`]([^"`]+)["`]/gm)].map((match) => match[1]));
  }
  return imports;
}

function goModulePath(source) {
  const match = source.match(/^module\s+(\S+)$/m);
  assert.ok(match, "missing Go module declaration");
  return match[1];
}

test("Go services and Console BFF remain physically isolated behind typed owner contracts", async () => {
  const rootModulePath = goModulePath(await text("go.mod"));
  assert.equal(rootModulePath, "opl-cloud", "root Go module owner");
  const sharedModules = ["postgresmigrate", "ownerservice", "ownerstore", "protectedresource", "tkeapply"];
  const sharedModulePaths = sharedModules.map((name) => `${rootModulePath}/services/internal/${name}`);

  const domainServices = (await readdir(new URL("services", root), { withFileTypes: true }))
    .filter((entry) => entry.isDirectory() && entry.name !== "internal")
    .map((entry) => entry.name)
    .sort();
  assert.ok(domainServices.length >= 10, `expected all domain services, saw ${domainServices.join(",")}`);

  const units: Array<{ name: string; directory: string; modulePath: string; isFabric: boolean }> = [
    ...domainServices.map((service) => ({
      name: service,
      directory: `services/${service}`,
      modulePath: `${rootModulePath}/services/${service}`,
      isFabric: service === "fabric",
    })),
    {
      name: "console-bff",
      directory: "apps/console-bff",
      modulePath: `${rootModulePath}/apps/console-bff`,
      isFabric: false,
    },
  ];

  // Production Go sources (excluding _test.go cross-owner live integration fixtures)
  // must stay physically isolated behind packages/contracts and allowlisted shared
  // domain-neutral internals.
  for (const unit of units) {
    const files = await filesUnder(unit.directory, (path) => path.endsWith(".go") && !path.endsWith("_test.go"));
    for (const file of files) {
      const source = await text(file);
      for (const imported of goImports(source)) {
        if (imported.startsWith(`${rootModulePath}/services/`) || imported.startsWith(`${rootModulePath}/apps/`)) {
          const owned = imported === unit.modulePath || imported.startsWith(`${unit.modulePath}/`);
          const shared = sharedModulePaths.some((path) => imported === path || imported.startsWith(`${path}/`));
          assert.equal(owned || shared, true, `${file} crosses into ${imported}`);
        }
        if (!unit.isFabric) {
          assert.equal(imported.startsWith("github.com/tencentcloud/"), false, `${file} imports Tencent Cloud SDK ${imported} outside Fabric`);
          assert.equal(imported.startsWith("github.com/moby/"), false, `${file} imports Docker SDK ${imported} outside Fabric`);
          assert.equal(imported.startsWith("k8s.io/"), false, `${file} imports raw k8s.io SDK ${imported} directly; use tkeapply`);
        }
      }
    }
  }

  for (const module of sharedModules) {
    for (const file of await filesUnder(`services/internal/${module}`, (path) => path.endsWith(".go"))) {
      assert.doesNotMatch(
        await text(file),
        /opl-cloud\/(?:services\/(?!internal\/)|apps\/)/,
        `${file} must remain domain-neutral`,
      );
    }
  }

  // The unified single-binary composition root imports only the composition
  // supervisor, shared contracts, and thin <unit>/module entrypoints—never
  // service internals or infrastructure SDKs.
  const allowedModuleImports = new Set([
    `${rootModulePath}/services/cloud-composition/composition`,
    ...units.map((unit) => `${unit.modulePath}/module`),
  ]);
  for (const file of await filesUnder("cmd/opl-cloud", (path) => path.endsWith(".go") && !path.endsWith("_test.go"))) {
    const source = await text(file);
    for (const imported of goImports(source)) {
      if (imported.startsWith(`${rootModulePath}/services/`) || imported.startsWith(`${rootModulePath}/apps/`)) {
        assert.equal(allowedModuleImports.has(imported), true, `${file} must import only thin module packages or composition, saw ${imported}`);
      }
      assert.equal(imported.includes("/internal/"), false, `${file} bypasses module boundary by importing ${imported}`);
      assert.equal(imported.startsWith("github.com/tencentcloud/"), false, `${file} imports Tencent Cloud SDK ${imported}`);
      assert.equal(imported.startsWith("github.com/moby/"), false, `${file} imports Docker SDK ${imported}`);
      assert.equal(imported.startsWith("k8s.io/"), false, `${file} imports raw k8s.io SDK ${imported}`);
    }
  }

  // Domain modules read configuration strictly through injected ModuleEnv so
  // in-process composition enforces allowlisted environment isolation.
  const cliSubcommandFiles = new Set([
    "services/serve/module/probe.go",
    "services/workspace/module/retry_initial_runtime.go",
  ]);
  for (const unit of units) {
    let moduleFiles: string[] = [];
    try {
      moduleFiles = await filesUnder(`${unit.directory}/module`, (path) => path.endsWith(".go") && !path.endsWith("_test.go"));
    } catch {
      continue;
    }
    for (const file of moduleFiles) {
      if (cliSubcommandFiles.has(file)) continue;
      const source = await text(file);
      assert.doesNotMatch(source, /\bos\.(?:Getenv|LookupEnv)\(/, `${file} must read environment via ModuleEnv, not direct os.Getenv/os.LookupEnv`);
    }
  }
});

test("Console UI reaches services only through the same-origin Control Plane API adapter", async () => {
  const networkOwner = "apps/console-ui/src/api";

  assert.equal(controlPlaneApiPath("/api/workspaces?page=1"), "/api/workspaces?page=1");
  for (const invalid of [
    "https://gflabtoken.cn/api/users",
    "/fabric/catalog",
    "/ledger/receipts",
    "/api/../fabric/catalog",
    "/api/%2e%2e/ledger/receipts"
  ]) {
    assert.throws(() => controlPlaneApiPath(invalid), /control_plane_api_path_required/, invalid);
  }

  const files = await filesUnder("apps/console-ui/src", (path) => path.endsWith(".ts") || path.endsWith(".tsx"));
  for (const file of files) {
    const source = await text(file);
    const specifiers = [...source.matchAll(/(?:from\s+|import\s*\()\s*["']([^"']+)["']/g)].map((match) => match[1]);
    for (const specifier of specifiers) {
      assert.doesNotMatch(specifier, /(?:^|\/)services\/(?:control-plane|fabric|ledger|internal)(?:\/|$)/, `${file} runtime deep import`);
      assert.doesNotMatch(specifier, /(?:^|\/)packages\/contracts(?:\/|$)/, `${file} contract deep import`);
    }

    const networkMarkers = ["fetch(", "WebSocket(", "EventSource(", "XMLHttpRequest(", "sendBeacon("];
    if (networkMarkers.some((marker) => source.includes(marker))) {
      assert.equal(file.startsWith(`${networkOwner}/`), true, `${file} performs network access outside the API adapter`);
    }
    for (const match of source.matchAll(/\bfetch\(\s*(["'`])([^"'`]+)\1/g)) {
      assert.equal(match[2].startsWith("/api/"), true, `${file} calls a non-Control-Plane URL: ${match[2]}`);
    }
  }

  const transport = await text("apps/console-ui/src/api/console-api.ts");
  assert.equal((transport.match(/fetch\(controlPlaneApiPath\(path\)/g) || []).length, 2);
});

import assert from "node:assert/strict";
import { access, readFile, readdir } from "node:fs/promises";
import test from "node:test";

// The runnable product image must ship an executable for every owner process the
// deployment can select. Instance chooses which processes to run, so an owner the
// image silently stops building is an installation that cannot start that
// capability. The expected owner set is derived from the repository layout, not
// restated here, so this tracks the source tree instead of a copied list.

async function ownerModulePackages(): Promise<Array<{ name: string; binary: string; importPath: string }>> {
  const modules: Array<{ name: string; binary: string; importPath: string }> = [];
  const entries = await readdir("services", { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    try {
      await access(`services/${entry.name}/module/run.go`);
      modules.push({
        name: entry.name,
        binary: `opl-${entry.name}`,
        importPath: `opl-cloud/services/${entry.name}/module`
      });
    } catch {
      // Skip non-daemon support directories such as internal/ or cloud-composition/.
    }
  }
  await access("apps/console-bff/module/run.go");
  modules.push({
    name: "console-bff",
    binary: "opl-console-bff",
    importPath: "opl-cloud/apps/console-bff/module"
  });
  return modules.sort((a, b) => a.name.localeCompare(b.name));
}

test("the product image compiles the unified opl-cloud binary with owner symlinks and no Node runtime bloat", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const goBuildStart = dockerfile.indexOf("AS go-build");
  const goBuildEnd = dockerfile.indexOf("FROM docker:");
  assert.ok(goBuildStart > 0 && goBuildEnd > goBuildStart, "Dockerfile must keep a single go-build stage before the docker-cli stage");
  const goBuildStage = dockerfile.slice(goBuildStart, goBuildEnd).replace(/\\\r?\n/g, " ");

  assert.match(goBuildStage, /COPY cmd\/opl-cloud \/src\/cmd\/opl-cloud/, "go-build must copy the single-binary composition root");
  assert.match(goBuildStage, /-o \/out\/opl-cloud \.\/cmd\/opl-cloud\b/, "go-build must build the unified /out/opl-cloud binary");
  assert.match(goBuildStage, /ln -s opl-cloud \/out\/opl-cloud-composition\b/, "go-build must symlink opl-cloud-composition to opl-cloud");

  const symlinkLoop = goBuildStage.match(/for bin in ([^;]+); do\s+ln -s opl-cloud "\/out\/\$bin"/);
  assert.ok(symlinkLoop, "go-build must symlink every owner entrypoint to /out/opl-cloud");
  const symlinkedOwners = symlinkLoop[1].trim().split(/\s+/).sort();

  const expectedModules = await ownerModulePackages();
  assert.ok(expectedModules.length >= 11, `expected all domain owner modules, saw ${expectedModules.map((m) => m.name).join(",")}`);
  assert.deepEqual(
    symlinkedOwners,
    expectedModules.map((m) => m.binary).sort(),
    "every domain module must receive an opl-<service> symlink to opl-cloud"
  );

  const cmdSource = await readFile("cmd/opl-cloud/main.go", "utf8");
  for (const mod of expectedModules) {
    assert.ok(cmdSource.includes(`"${mod.importPath}"`), `cmd/opl-cloud/main.go must register ${mod.importPath}`);
    assert.ok(cmdSource.includes(`"${mod.binary}"`), `cmd/opl-cloud/main.go must dispatch ${mod.binary}`);
  }

  for (const [binary, command] of [
    ["opl-cloud", "./cmd/opl-cloud"],
    ["opl-tencent-provisioner", "./cmd/opl-tencent-provisioner"],
    ["opl-node-image-retire", "./cmd/opl-node-image-retire"],
    ["opl-owner-probe", "./cmd/opl-owner-probe"]
  ]) {
    assert.ok(goBuildStage.includes(`-o /out/${binary} ${command}`), `go-build must build ${binary}`);
  }

  // The runtime image must stay free of Node.js / npm dependencies and ship all Go binaries, symlinks and topology contract.
  const runtimeStage = dockerfile.slice(dockerfile.lastIndexOf("FROM "));
  assert.match(runtimeStage, /FROM debian:bookworm-slim@sha256:/, "the runtime stage must use slim Debian instead of Node");
  assert.ok(!runtimeStage.includes("npm ci"), "the runtime stage must not install npm packages");
  assert.match(runtimeStage, /COPY --from=go-build \/out\/ \/usr\/local\/bin\//, "the runtime stage must copy all built Go binaries and symlinks");
  assert.match(runtimeStage, /COPY deploy\/portable\/opl-cloud-owner-topology\.json \/app\/contracts\/opl-cloud-owner-topology\.json/, "the runtime stage must bake the owner topology contract");
});

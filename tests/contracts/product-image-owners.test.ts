import assert from "node:assert/strict";
import { access, readFile, readdir } from "node:fs/promises";
import test from "node:test";

// The runnable product image must ship an executable for every owner process the
// deployment can select. Instance chooses which processes to run, so an owner the
// image silently stops building is an installation that cannot start that
// capability. The expected owner set is derived from the repository layout, not
// restated here, so this tracks the source tree instead of a copied list.

async function ownerServicesWithServer() {
  const entries = await readdir("services", { withFileTypes: true });
  const services = [];
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    try {
      await access(`services/${entry.name}/cmd/server/main.go`);
      services.push(entry.name);
    } catch {
      // A service without a server entrypoint is not an independently runnable owner.
    }
  }
  return services.sort();
}

test("the product image builds every service and control binary in the single go-build stage without Node runtime bloat", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const goBuildStart = dockerfile.indexOf("AS go-build");
  const goBuildEnd = dockerfile.indexOf("FROM docker:");
  assert.ok(goBuildStart > 0 && goBuildEnd > goBuildStart, "Dockerfile must keep a single go-build stage before the docker-cli stage");
  const goBuildStage = dockerfile.slice(goBuildStart, goBuildEnd);

  // control-plane, fabric and ledger are built explicitly alongside the owner
  // server loop inside the single shared Go build stage.
  const dedicated = new Set(["control-plane", "fabric", "ledger"]);
  const expected = (await ownerServicesWithServer()).filter((service) => !dedicated.has(service));
  assert.ok(expected.length >= 7, `expected the target owner set, saw ${expected.join(",")}`);

  const loop = goBuildStage.match(/for service in ([^;]+); do/);
  assert.ok(loop, "the go-build stage must iterate its owner set");
  const built = loop[1].trim().split(/\s+/).sort();
  assert.deepEqual(built, expected, "every owner with a server entrypoint must be built in go-build");
  assert.ok(
    goBuildStage.includes("-o /out/opl-$service ./cmd/server"),
    "go-build must emit one opl-<service> binary per iteration"
  );
  assert.match(goBuildStage, /-o \/out\/opl-console-bff \.\/cmd\/server/, "go-build must emit the Console BFF");
  for (const [binary, command] of [
    ["opl-control-plane", "./cmd/control-plane"],
    ["opl-fabric", "./cmd/fabric"],
    ["opl-ledger", "./cmd/ledger"],
    ["opl-tencent-provisioner", "./cmd/opl-tencent-provisioner"],
    ["opl-node-image-retire", "./cmd/opl-node-image-retire"],
    ["opl-owner-probe", "./cmd/opl-owner-probe"],
    ["opl-cloud-composition", "./cmd/opl-cloud-composition"]
  ]) {
    assert.ok(goBuildStage.includes(`-o /out/${binary} ${command}`), `go-build must build ${binary}`);
  }

  // The runtime image must stay free of Node.js / npm dependencies and ship all Go binaries and topology contract.
  const runtimeStage = dockerfile.slice(dockerfile.lastIndexOf("FROM "));
  assert.match(runtimeStage, /FROM debian:bookworm-slim@sha256:/, "the runtime stage must use slim Debian instead of Node");
  assert.ok(!runtimeStage.includes("npm ci"), "the runtime stage must not install npm packages");
  assert.match(runtimeStage, /COPY --from=go-build \/out\/ \/usr\/local\/bin\//, "the runtime stage must copy all built Go binaries");
  assert.match(runtimeStage, /COPY deploy\/portable\/opl-cloud-owner-topology\.json \/app\/contracts\/opl-cloud-owner-topology\.json/, "the runtime stage must bake the owner topology contract");
});

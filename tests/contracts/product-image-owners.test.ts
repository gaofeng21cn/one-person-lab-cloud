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

test("the product image builds every service that has a server entrypoint", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const publisherStart = dockerfile.indexOf("AS publisher-build");
  const publisherEnd = dockerfile.indexOf("FROM docker:27.5.1-cli");
  assert.ok(publisherStart > 0 && publisherEnd > publisherStart, "Dockerfile must keep its publisher-build stage before the docker-cli stage");
  const publisherStage = dockerfile.slice(publisherStart, publisherEnd);

  // control-plane, fabric and ledger are built by their own stages; every other
  // owner with a server entrypoint is built in the shared publisher lane.
  const dedicated = new Set(["control-plane", "fabric", "ledger"]);
  const expected = (await ownerServicesWithServer()).filter((service) => !dedicated.has(service));
  assert.ok(expected.length >= 7, `expected the target owner set, saw ${expected.join(",")}`);

  const loop = publisherStage.match(/for service in ([^;]+); do/);
  assert.ok(loop, "the publisher-build stage must iterate its owner set");
  const built = loop[1].trim().split(/\s+/).sort();
  assert.deepEqual(built, expected, "every owner with a server entrypoint must be built in the publisher lane");
  // The lane builds the loop variable, so one emitted name per iteration is the
  // exact shape the installer relies on.
  assert.ok(
    publisherStage.includes("-o /out/opl-$service ./cmd/server"),
    "publisher-build must emit one opl-<service> binary per iteration"
  );
  assert.match(publisherStage, /-o \/out\/opl-console-bff \.\/cmd\/server/, "publisher-build must emit the Console BFF");

  // The runtime image is what Instance actually runs, so the binaries must be copied there.
  const runtimeStage = dockerfile.slice(dockerfile.indexOf(" AS runtime"));
  assert.match(runtimeStage, /COPY --from=publisher-build \/out\/ \/usr\/local\/bin\//, "the runtime stage must copy the publisher binaries");
});

test("the product image builds the three control services from their own stages", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  for (const [stage, binary, command] of [
    ["control-plane-build", "opl-control-plane", "./cmd/control-plane"],
    ["fabric-build", "opl-fabric", "./cmd/fabric"],
    ["ledger-build", "opl-ledger", "./cmd/ledger"]
  ]) {
    const start = dockerfile.indexOf(` AS ${stage}`);
    const end = dockerfile.indexOf("FROM ", start + 5);
    const body = dockerfile.slice(start, end === -1 ? undefined : end);
    assert.ok(body.includes(`-o /out/${binary} ${command}`), `${stage} must build ${binary}`);
    assert.ok(dockerfile.includes(`COPY --from=${stage} /out/${binary} /usr/local/bin/${binary}`), `${binary} must be copied into the runtime image`);
  }
});

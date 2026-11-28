import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("OPL Cloud product image contains all three control-service binaries", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const dockerignore = (await readFile(".dockerignore", "utf8"))
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  const singleBinary = await readFile("cmd/opl-cloud/main.go", "utf8");
  assert.match(dockerfile, /go build[^\n]*-ldflags "-X opl-cloud\/packages\/contracts\/go\.ProductSourceSHA=\$OPL_SOURCE_SHA" -o \/out\/opl-cloud \.\/cmd\/opl-cloud/);
  for (const [serviceModule, binary] of [
    ["opl-cloud/services/control-plane/module", "opl-control-plane"],
    ["opl-cloud/services/fabric/module", "opl-fabric"],
    ["opl-cloud/services/ledger/module", "opl-ledger"]
  ]) {
    assert.ok(dockerfile.includes(binary), `Dockerfile must ship symlink for ${binary}`);
    assert.ok(singleBinary.includes(serviceModule), `cmd/opl-cloud must register ${serviceModule} for ${binary}`);
  }
  assert.match(dockerfile, /COPY --from=go-build \/out\/ \/usr\/local\/bin\//);
  assert.match(dockerfile, /ARG TARGETARCH/);
  assert.match(dockerfile, /linux\/\$\{TARGETARCH\}\/kubectl/);
  for (const ignored of ["node_modules", "dist", ".runtime", ".git", ".env"]) {
    assert.ok(dockerignore.includes(ignored), `.dockerignore missing ${ignored}`);
  }
});

test("OPL Cloud product image carries the buildx CLI plugin for Agent OCI builds", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  // The Build owner runs `docker buildx build`; the docker CLI finds buildx only
  // as a CLI plugin on its plugin path. Shipping only the docker binary makes
  // every Agent build fail with "unknown command: buildx".
  assert.match(
    dockerfile,
    /COPY --from=docker-cli \/usr\/local\/libexec\/docker\/cli-plugins\/docker-buildx \/usr\/local\/libexec\/docker\/cli-plugins\/docker-buildx/
  );
});

test("Go build stage uses the unified root go.mod and copies Go contracts before compiling", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const goBuildStage = dockerfile.slice(dockerfile.indexOf("AS go-build"), dockerfile.indexOf("FROM docker:"));
  const rootGoMod = "COPY go.mod go.sum /src/";
  const contractsCopy = "COPY packages/contracts/go /src/packages/contracts/go";
  const dependencyDownload = 'RUN GOPROXY="$GOPROXY" go mod download';

  assert.ok(goBuildStage.includes(rootGoMod), "go-build stage must copy root go.mod and go.sum");
  assert.ok(goBuildStage.includes(contractsCopy), "go-build stage is missing shared Go contracts");
  assert.ok(
    goBuildStage.indexOf(rootGoMod) < goBuildStage.indexOf(dependencyDownload),
    "go-build must copy root go.mod/go.sum before downloading modules"
  );
  assert.ok(
    goBuildStage.indexOf(contractsCopy) < goBuildStage.indexOf("go build"),
    "go-build must copy local Go contracts before compiling"
  );
});

import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";

// The product revision a running binary states about itself is only meaningful
// if every binary in the product image carries it, and if the revision the
// image is built from is the same one the image claims. These checks hold the
// build to that: the Dockerfile stamps every product binary, refuses to build
// without a valid revision, and the Candidate workflow passes the same
// revision it puts in the image label.

const STAMP = "-X opl-cloud/packages/contracts/go.ProductSourceSHA=$OPL_SOURCE_SHA";
const execFileAsync = promisify(execFile);

test("linked owner binaries retain the source stamp even without a revision reader", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "opl-source-stamp-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const source = join(directory, "main.go");
  const binary = join(directory, "owner");
  await writeFile(source, 'package main\nimport _ "opl-cloud/packages/contracts/go"\nfunc main() {}\n');
  const sha = "0123456789abcdef0123456789abcdef01234567";
  await execFileAsync("go", ["build", "-ldflags", `-X opl-cloud/packages/contracts/go.ProductSourceSHA=${sha}`, "-o", binary, source], {
    cwd: "packages/contracts/go",
  });
  const { stdout } = await execFileAsync("go", ["tool", "nm", binary], { maxBuffer: 16 * 1024 * 1024 });
  assert.ok(/\bD opl-cloud\/packages\/contracts\/go\.ProductSourceSHA$/m.test(stdout), "the linked owner lost its ProductSourceSHA symbol");
  assert.ok(/\bR opl-cloud\/packages\/contracts\/go\.ProductSourceSHA\.str$/m.test(stdout), "the linked owner lost its stamped revision bytes");
  await execFileAsync(binary);
});

test("every product binary in the image carries the build-time product revision", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const buildCommands = [...dockerfile.matchAll(/go (?:-C [^ ]+ )?build[^\n]*/g)].map((match) => match[0]);
  const productBuilds = buildCommands.filter((command) => command.includes("-o /out/opl-"));
  // Three Fabric binaries, Control Plane, Ledger, the owner loop and the
  // Console BFF. The owner loop emits one binary per owner, so the thirteen
  // product binaries come from seven build commands.
  assert.equal(productBuilds.length, 7, `expected seven product build commands, saw ${productBuilds.length}`);

  for (const command of productBuilds) {
    assert.ok(
      command.includes(STAMP),
      `a product binary is built without the product revision stamp: ${command}`
    );
  }

  // The publisher lane builds one binary per iteration, so the stamp has to be
  // on that single command rather than on a per-service list.
  const publisherStart = dockerfile.indexOf("AS publisher-build");
  const publisherEnd = dockerfile.indexOf("FROM docker:");
  assert.ok(publisherStart > 0 && publisherEnd > publisherStart);
  const publisherStage = dockerfile.slice(publisherStart, publisherEnd);
  const publisherBuilds = [...publisherStage.matchAll(/go (?:-C [^ ]+ )?build[^\n]*/g)].map((match) => match[0]);
  assert.ok(publisherBuilds.length >= 2, "the publisher lane must build the owner services and the Console BFF");
  for (const command of publisherBuilds) {
    assert.ok(command.includes(STAMP), `the publisher lane builds a binary without the stamp: ${command}`);
  }
});

test("the product image refuses to build without a valid product revision", async () => {
  const dockerfile = await readFile("Dockerfile", "utf8");
  const stages = ["fabric-build", "control-plane-build", "ledger-build", "publisher-build"];
  for (const stage of stages) {
    const start = dockerfile.indexOf(`AS ${stage}`);
    assert.ok(start > 0, `the product image must keep its ${stage} stage`);
    const next = dockerfile.indexOf("\nFROM ", start);
    const body = dockerfile.slice(start, next > 0 ? next : dockerfile.length);
    assert.ok(body.includes("ARG OPL_SOURCE_SHA"), `${stage} must receive the product revision`);
    assert.ok(
      /printf '%s' "\$OPL_SOURCE_SHA" \| grep -Eq '\^\[0-9a-f\]\{40\}\$'/u.test(body),
      `${stage} must refuse a product revision that is not a source revision`
    );
  }
});

test("the Candidate workflow builds the image from the revision it names in the image label", async () => {
  const workflow = await readFile(".github/workflows/build-opl-cloud-candidate.yml", "utf8");
  assert.ok(
    workflow.includes('--build-arg OPL_SOURCE_SHA="$PRODUCT_SHA"'),
    "the Candidate build must stamp the binaries from the product revision it builds"
  );
  assert.ok(
    workflow.includes('--label "org.opencontainers.image.revision=$PRODUCT_SHA"'),
    "the Candidate image must keep naming the revision it was built from"
  );
  // The workflow already refuses to build from a checkout that is not the
  // claimed revision, so the stamped revision and the built revision are the
  // same commit.
  assert.ok(
    workflow.includes('"$(git rev-parse HEAD)" != "$PRODUCT_SHA"'),
    "the Candidate build must prove the checkout is the claimed revision before it stamps it"
  );
});

test("the running binary states its own revision at the attestation boundary", async () => {
  const provisioner = await readFile("services/fabric/cmd/opl-tencent-provisioner/main.go", "utf8");
  assert.ok(
    provisioner.includes("contracts.ProductSourceSHAIsCurrent(env[\"OPL_RELEASE_SHA\"])"),
    "the pre-debit IAM gate must compare the claimed release with the running binary's own revision"
  );
  const contracts = await readFile("packages/contracts/go/release.go", "utf8");
  assert.ok(
    contracts.includes("var ProductSourceSHA = \"\""),
    "the product revision must be a stamp the build sets"
  );
});

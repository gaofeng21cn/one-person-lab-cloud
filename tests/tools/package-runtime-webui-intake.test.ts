import assert from "node:assert/strict";
import test from "node:test";

import { admitChain, admitDevelopmentFixture } from "../../tools/package-runtime-webui-intake.ts";

const digest = (c: string) => `sha256:${c.repeat(64)}`;

function completeChain() {
  return {
    package: {
      packageId: "oma",
      versionLabel: "0.4.12",
      manifestDigest: digest("a"),
      contentDigest: digest("b")
    },
    runtime: {
      runtimeReleaseId: "runtime-release-1",
      imageDigest: digest("c"),
      runtimeAbiVersion: "opl-runtime-abi/v1",
      packageFormatVersions: ["opl-agent-package/v1"]
    },
    webui: {
      webuiVersionId: "webui-1",
      artifactDigest: digest("d"),
      port: 3000,
      healthPath: "/healthz",
      runtimeAbiVersions: ["opl-runtime-abi/v1"]
    },
    cloudSourceSha: "e".repeat(40)
  };
}

test("a complete immutable and compatible chain is admitted", () => {
  assert.deepEqual(admitChain(completeChain()), { admitted: true });
});

test("a mutable tag instead of a Runtime image digest is refused", () => {
  const chain = completeChain();
  chain.runtime.imageDigest = "ghcr.io/example/runtime:latest";
  const result = admitChain(chain);
  assert.equal(result.admitted, false);
  assert.ok(result.refusals.some((r) => r.reason === "runtime_image_digest_not_immutable"));
});

test("a mutable WebUI artifact (missing digest) is refused", () => {
  const chain = completeChain();
  (chain.webui as { artifactDigest?: string }).artifactDigest = undefined;
  const result = admitChain(chain);
  assert.equal(result.admitted, false);
  assert.ok(result.refusals.some((r) => r.reason === "webui_artifact_digest_not_immutable"));
});

test("a WebUI port other than 3000 is refused", () => {
  const chain = completeChain();
  chain.webui.port = 8080;
  const result = admitChain(chain);
  assert.equal(result.admitted, false);
  assert.ok(result.refusals.some((r) => r.reason === "webui_port_not_3000"));
});

test("a Runtime ABI the WebUI does not declare is a compatibility refusal", () => {
  const chain = completeChain();
  chain.runtime.runtimeAbiVersion = "opl-runtime-abi/v2";
  const result = admitChain(chain);
  assert.equal(result.admitted, false);
  assert.ok(result.refusals.some((r) => r.reason === "runtime_abi_not_supported_by_webui"));
});

test("a missing package content digest is refused instead of derived", () => {
  const chain = completeChain();
  (chain.package as { contentDigest?: string }).contentDigest = "";
  const result = admitChain(chain);
  assert.equal(result.admitted, false);
  assert.ok(result.refusals.some((r) => r.reason === "package_content_digest_not_immutable"));
});

test("an IBD candidate may only be used as an explicitly labelled development fixture", () => {
  assert.deepEqual(admitDevelopmentFixture({ labelled: true, provenance: "legacy" }), { admitted: true });
  const unlabelled = admitDevelopmentFixture({ labelled: false, provenance: "legacy" });
  assert.equal(unlabelled.admitted, false);
  const unknown = admitDevelopmentFixture({ labelled: true, provenance: "production" });
  assert.equal(unknown.admitted, false);
});

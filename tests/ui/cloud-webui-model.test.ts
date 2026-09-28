import assert from "node:assert/strict";
import test from "node:test";
import { admitWebuiSelection, isImmutableDigest } from "../../apps/console-ui/src/app/cloud-webui-model.ts";

const digest = "sha256:" + "a".repeat(64);
const webui = { id: "webui-1", status: "approved", artifactDigest: digest, port: 3000, healthPath: "/healthz", runtimeAbiVersions: ["opl-runtime-abi/v1"], packageFormatVersions: ["opl-agent-package/v1"] };
const runtime = { id: "runtime-1", status: "approved", artifactDigest: digest, runtimeAbiVersion: "opl-runtime-abi/v1", packageFormatVersions: ["opl-agent-package/v1"] };

test("Cloud WebUI admission accepts exact immutable compatible inputs", () => {
  assert.deepEqual(admitWebuiSelection(webui, runtime), { accepted: true, reasons: [] });
  assert.equal(isImmutableDigest(digest), true);
});

test("Cloud WebUI admission refuses mutable or incompatible inputs", () => {
  const decision = admitWebuiSelection({ ...webui, artifactDigest: "ghcr.io/example/webui:latest", port: 8080 }, { ...runtime, runtimeAbiVersion: "opl-runtime-abi/v2" });
  assert.deepEqual(decision.reasons, ["webui_artifact_digest_not_immutable", "webui_port_not_3000", "runtime_abi_not_supported_by_webui"]);
});

import { readdir, readFile } from "node:fs/promises";
import YAML from "yaml";

import { assetNames } from "../deploy/portable/installer-manifest.mjs";

const root = new URL("../", import.meta.url);
const workflowRoot = new URL(".github/workflows/", root);
const allowedWorkflows = new Set([
  "build-cloud-agent-webui.yml",
  "build-opl-cloud-candidate.yml",
  "clean-host-qualification.yml",
  "cloud-business-readback.yml",
  "codeql.yml",
  "daily-release.yml",
  "pull-request-ci.yml",
  "qualification.yml",
  "release-opl-cloud-image.yml",
  "release-opl-cloud-public-readback.yml",
  "whitepaper.yml",
  "warm-ci-caches.yml",
  "workspace-compute-readback-handoff.yml"
]);

const workflows = (await readdir(workflowRoot)).filter((name) => name.endsWith(".yml"));
// A second build workflow bypassed the Candidate guard once (an actor-pinned,
// single-branch bridge). Keep exactly one Product-image build entry. The separate
// WebUI input below must not become another Product builder or deployment lane.
const buildWorkflows = workflows.filter((name) => name.startsWith("build-opl-cloud")).sort();
if (JSON.stringify(buildWorkflows) !== JSON.stringify(["build-opl-cloud-candidate.yml"])) {
  throw new Error(`exactly one Cloud build workflow is allowed: ${buildWorkflows.join(",") || "none"}`);
}
const unexpectedWorkflows = workflows.filter((name) => !allowedWorkflows.has(name));
if (unexpectedWorkflows.length > 0) {
  throw new Error(`unexpected Cloud workflow: ${unexpectedWorkflows.join(",")}`);
}

// The reused publisher credential has broader permissions than these reads
// require. Keep every reference behind the existing main-only owner gate and
// its exact protected environment; never expose it to Candidate or PR jobs.
const credentialEnvironments = {
  "release-opl-cloud-image.yml:admission": "cloud-release",
  "daily-release.yml:continue": "cloud-release",
  "cloud-business-readback.yml:readback": "cloud-business-qualification",
  "workspace-compute-readback-handoff.yml:admission": "cloud-release"
};
for (const name of workflows) {
  const workflow = YAML.parse(await readFile(new URL(name, workflowRoot), "utf8"));
  if (JSON.stringify(workflow.env || {}).includes("secrets.OPL_GITHUB_RELEASE_ADMIN_TOKEN")) {
    throw new Error("Cloud publisher credential cannot be exposed at workflow scope");
  }
  for (const [id, job] of Object.entries(workflow.jobs || {})) {
    if (!JSON.stringify(job).includes("secrets.OPL_GITHUB_RELEASE_ADMIN_TOKEN")) continue;
    const environment = credentialEnvironments[`${name}:${id}`];
    const manualGate = "${{ github.ref == 'refs/heads/main' && github.actor == github.triggering_actor && (github.actor == github.repository_owner || github.actor == 'RenDeHuang') }}";
    const gates = {
      "release-opl-cloud-image.yml:admission": "${{ github.ref == 'refs/heads/main' && github.actor == github.triggering_actor && (github.actor == github.repository_owner || github.actor == 'RenDeHuang' || (github.actor == 'github-actions[bot]' && inputs.automation_source_run_id != '')) }}",
      "daily-release.yml:continue": "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main'"
    };
    if (!environment || job.environment !== environment || job.if !== (gates[`${name}:${id}`] || manualGate)) {
      throw new Error(`Cloud publisher credential requires its protected environment and owner gate: ${name}:${id}`);
    }
  }
}

const webuiWorkflow = YAML.parse(await readFile(new URL(".github/workflows/build-cloud-agent-webui.yml", root), "utf8"));
const webuiJob = webuiWorkflow.jobs?.artifact;
const webuiCommands = (webuiJob?.steps || []).map(step => step.run || "").join("\n");
if (JSON.stringify(Object.keys(webuiWorkflow.on || {})) !== JSON.stringify(["workflow_dispatch"]) ||
    JSON.stringify(Object.keys(webuiWorkflow.on?.workflow_dispatch?.inputs || {})) !== JSON.stringify(["product_sha"]) ||
    webuiWorkflow.on.workflow_dispatch.inputs.product_sha.required !== true ||
    JSON.stringify(Object.keys(webuiWorkflow.jobs || {})) !== JSON.stringify(["artifact"]) ||
    webuiJob?.if !== "${{ github.ref_type == 'branch' && github.sha == inputs.product_sha && github.actor == github.triggering_actor }}" ||
    webuiJob?.["runs-on"] !== "ubuntu-latest" || webuiJob?.environment !== undefined ||
    JSON.stringify(webuiJob?.permissions) !== JSON.stringify({ contents: "read", packages: "write" }) ||
    !webuiCommands.includes('git archive "$PRODUCT_SHA" webui/cloud-agent') ||
    !webuiCommands.includes('printf \'BUILD_CONTEXT=%s/webui/cloud-agent\\n\' "$build_context"') ||
    !webuiCommands.includes('IMAGE_REPOSITORY=ghcr.io/%s/opl-cloud-agent-webui') ||
    !webuiCommands.includes('sha256sum build-metadata.json source.txt index.json amd64.image.json arm64.image.json') ||
    JSON.stringify(webuiWorkflow).includes("secrets.")) {
  throw new Error("Cloud Agent WebUI workflow authority boundary is invalid");
}

const release = await readFile(new URL(".github/workflows/release-opl-cloud-image.yml", root), "utf8");
const releaseWorkflow = YAML.parse(release);
const releaseAdmission = releaseWorkflow.jobs?.admission;
const releasePublish = releaseWorkflow.jobs?.publish;
const readback = await readFile(new URL(".github/workflows/release-opl-cloud-public-readback.yml", root), "utf8");
const readbackWorkflow = YAML.parse(readback);
const readbackJob = readbackWorkflow.jobs?.["public-readback"];
if (JSON.stringify(Object.keys(releaseWorkflow.jobs || {})) !== JSON.stringify(["admission", "publish"]) ||
    releaseWorkflow.concurrency !== undefined || releaseAdmission?.environment !== "cloud-release" || releaseAdmission?.concurrency !== undefined ||
    releasePublish?.environment !== "cloud-release" || releasePublish?.needs !== "admission" ||
    releasePublish?.concurrency?.group !== "opl-cloud-publication-global" ||
    JSON.stringify(Object.keys(readbackWorkflow.jobs || {})) !== JSON.stringify(["public-readback"]) ||
    JSON.stringify(readbackWorkflow.on?.workflow_run?.workflows) !== JSON.stringify(["Release OPL Cloud"]) ||
    JSON.stringify(readbackWorkflow.on?.workflow_run?.types) !== JSON.stringify(["completed"]) ||
    readbackWorkflow.on?.workflow_dispatch?.inputs?.publication_run_id?.required !== true ||
    readbackJob?.environment !== undefined || readbackJob?.concurrency !== undefined ||
    readbackJob?.permissions?.contents !== "read" || readbackJob?.permissions?.actions !== "read" ||
    readbackJob?.permissions?.attestations !== "read") {
  throw new Error("Cloud Release stage and publication serialization boundary is invalid");
}
const releaseCommands = Object.values(releaseWorkflow.jobs || {}).flatMap((job) => job.steps || []).map((step) => step.run || "").join("\n");
const readbackCommands = (readbackJob?.steps || []).map((step) => step.run || "").join("\n");
if (releaseCommands.includes("docker buildx build")) {
  throw new Error("Cloud Release must promote qualified Candidate bytes without rebuilding them");
}
for (const required of ["ghcr.io/${{ github.repository }}", "docker buildx imagetools create", "gh release create", "--clobber"]) {
  if (!release.includes(required)) throw new Error("Cloud release is missing: " + required);
}
const releaseInputs = releaseWorkflow.on?.workflow_dispatch?.inputs || {};
const expectedReleaseInputs = ["version", "ref", "candidate_run_id", "local_qualification_run_id", "instance_deployment_verification_run_id", "source_verification_run_id"];
if (JSON.stringify(Object.keys(releaseInputs)) !== JSON.stringify([...expectedReleaseInputs, "automation_source_run_id"]) || expectedReleaseInputs.some((name) => releaseInputs[name]?.required !== true)) {
  throw new Error("Cloud Release requires exact Candidate, local installation, Instance installation and source CI evidence");
}
const installationDownload = (releaseAdmission?.steps || []).find((step) => step.name === "Download exact Instance installation verification");
if (installationDownload?.with?.repository !== "gaofeng21cn/opl-instance-medopl" ||
    installationDownload?.with?.["github-token"] !== "${{ secrets.OPL_GITHUB_RELEASE_ADMIN_TOKEN }}" ||
    installationDownload?.with?.["run-id"] !== "${{ inputs.instance_deployment_verification_run_id }}") {
  throw new Error("Cloud Release must download the original protected Instance installation receipt");
}
const installationStep = String((releaseAdmission?.steps || []).find((step) => step.id === "installation")?.run || "");
if (!installationStep.includes("deployment-verification-receipt.mjs") || !installationStep.includes("validateReleaseInstallationReadback")) {
  throw new Error("Cloud Release must keep native Instance validation and exact Candidate binding");
}
const sourceStep = String((releaseAdmission?.steps || []).find((step) => step.id === "source_paths")?.run || "");
const sealStep = String((releaseAdmission?.steps || []).find((step) => step.id === "release_bundle")?.run || "");
const publishValidate = String((releasePublish?.steps || []).find((step) => step.name === "Validate exact release bundle")?.run || "");
if (!sourceStep.includes("verifyReleaseSourcePaths") || !sealStep.includes("installation_and_source_paths") ||
    !publishValidate.includes("paidBusinessExecuted == false") || !publishValidate.includes("customerReadsExecuted == false") ||
    !readbackCommands.includes("paidBusinessExecuted == false")) {
  throw new Error("Cloud Release and public readback must disclose source-only business acceptance");
}
if (releasePublish?.steps?.some((step) => String(step.run || "").includes("curl -fsSL")) || !readbackCommands.includes("gh attestation verify")) {
  throw new Error("Cloud public readback must remain in its independent follower");
}

const candidate = await readFile(new URL(".github/workflows/build-opl-cloud-candidate.yml", root), "utf8");
const candidateWorkflow = YAML.parse(candidate);
const candidateInputs = Object.keys(candidateWorkflow.on?.workflow_dispatch?.inputs || {});
const candidateJobs = Object.keys(candidateWorkflow.jobs || {});
const candidateJob = candidateWorkflow.jobs?.candidate;
if (JSON.stringify(candidateInputs) !== JSON.stringify(["product_sha", "release_tag"]) ||
    JSON.stringify(candidateJobs) !== JSON.stringify(["candidate"]) ||
    candidateJob?.environment !== undefined ||
    candidateJob?.env?.IMAGE_REPOSITORY !== "ghcr.io/${{ github.repository }}" ||
    candidateJob?.permissions?.contents !== "read" || candidateJob?.permissions?.packages !== "write") {
  throw new Error("Cloud Candidate workflow authority boundary is invalid");
}
// Exact branch heads remain available to write-access collaborators. Main may
// continue a frozen annotated canonical tag with the same source identity.
const candidateGuard = String(candidateJob?.if ?? "");
for (const required of ["github.ref_type == 'branch'", "github.sha == inputs.product_sha", "github.actor == github.triggering_actor"]) {
  if (!candidateGuard.includes(required)) throw new Error(`Cloud Candidate guard is missing: ${required}`);
}
if (candidateGuard.includes("github.repository_owner")) {
  throw new Error("Cloud Candidate guard must not be owner-only; publication already admits a write-access collaborator");
}
// The build guard must not become an actor allowlist of its own. Only the release
// gate may name an actor, and naming one here would silently refuse the
// write-access collaborator this boundary admits while still satisfying the check
// above. This catches a literal account comparison, which the repository_owner
// check cannot see.
if (/github\.actor\s*(==|!=)\s*'[^']+'/.test(candidateGuard)) {
  throw new Error("Cloud Candidate guard must not name an actor; only the release gate may allowlist one");
}
if (!candidateGuard.includes("github.sha == inputs.product_sha || (github.ref == 'refs/heads/main' && inputs.release_tag != '')")) {
  throw new Error("Cloud Candidate must bind either the dispatch head or a frozen main product tag");
}
const candidateCommands = (candidateJob.steps || []).map((step) => step.run || "").join("\n");
const candidateArtifact = (candidateJob.steps || []).find((step) => step.id === "candidate_artifact");
if (candidateArtifact?.with?.name !== "opl-cloud-candidate-${{ inputs.product_sha }}-${{ github.run_attempt }}") {
  throw new Error("Cloud Candidate artifact locator does not include the workflow run attempt");
}
for (const required of ["--platform linux/amd64,linux/arm64", "--push", "tools/cloud-candidate-receipt.ts validate-bundle", "tools/cloud-portable-assets.ts checksums", "opl-cloud-candidate.json"]) {
  if (!candidateCommands.includes(required)) throw new Error(`Cloud Candidate is missing: ${required}`);
}
// The Candidate image is the shipped cloud product: it must bake the cloud
// Console identity explicitly and prove it on the emitted OCI, so a product
// image can never silently install the legacy Console bundle (no "智能体" nav).
if (!candidateCommands.includes("--build-arg VITE_CONSOLE_IDENTITY=cloud")) {
  throw new Error("Cloud Candidate must explicitly build the cloud Console identity");
}
if (!candidateCommands.includes('opl.cloud.console-identity"] == "cloud')) {
  throw new Error("Cloud Candidate must read back the cloud console identity from the emitted image");
}

const compose = await readFile(new URL("compose.yaml", root), "utf8");
for (const required of ["control-plane:", "fabric:", "ledger:", "postgres:", "OPL_CLOUD_IMAGE"]) {
  if (!compose.includes(required)) throw new Error(`portable Compose is missing: ${required}`);
}
const commonImage = compose.match(/^  image: (.+)$/m)?.[1] || "";
for (const service of ["control-plane", "fabric", "ledger", "postgres"]) {
  const block = compose.match(new RegExp(`^  ${service}:\\n([\\s\\S]*?)(?=^  [a-z][a-z0-9-]*:|^volumes:|^configs:|^networks:)`, "m"))?.[1] || "";
  const image = block.match(/^    image: (.+)$/m)?.[1] || commonImage;
  if (!/@sha256:[0-9a-f]{64}$/.test(image) && image !== "${OPL_CLOUD_IMAGE:?Set OPL_CLOUD_IMAGE to an immutable GHCR digest}") {
    throw new Error(`portable Compose image is not digest-pinned: ${service}`);
  }
}

const dockerfile = await readFile(new URL("Dockerfile", root), "utf8");
const baseImages = dockerfile
  .split("\n")
  .map((line) => line.trim())
  .filter((line) => line.startsWith("FROM"));
for (const line of baseImages) {
  if (!/@sha256:[0-9a-f]{64}\b/.test(line)) {
    throw new Error(`release base image is not digest-pinned: ${line}`);
  }
}
const kubectlAmd64 = dockerfile.match(/amd64\)\s*KUBECTL_SHA256="([0-9a-f]{64})"/);
const kubectlArm64 = dockerfile.match(/arm64\)\s*KUBECTL_SHA256="([0-9a-f]{64})"/);
if (!kubectlAmd64 || !kubectlArm64) {
  throw new Error("release kubectl download is not checksum-bound per architecture");
}
if (!/sha256sum\s+-c\s/.test(dockerfile)) {
  throw new Error("release kubectl download is not checksum-verified");
}

const contract = JSON.parse(await readFile(new URL("packages/contracts/opl-cloud-distribution-contract.json", root), "utf8"));
const candidateContract = JSON.parse(await readFile(new URL("packages/contracts/opl-cloud-candidate-receipt-contract.json", root), "utf8"));
const candidateReceipt = candidateContract.candidateReceiptV2;
const requiredEvidence = contract.distribution?.publication?.requiredEvidence || {};
const requiredEvidenceClasses = Object.keys(requiredEvidence);
const requiredEvidenceNames = Object.values(requiredEvidence).flat();
const bundleFormats = contract.bundleFormats;
const currentFormat = bundleFormats?.formats?.[bundleFormats?.current];
const legacyFormat = bundleFormats?.formats?.["legacy-v2"];
if (!currentFormat || !legacyFormat ||
    contract.distribution?.releaseAssets?.$ref !== "#/bundleFormats" ||
    candidateReceipt?.currentPortableAssets?.$ref !== "opl-cloud-distribution-contract.json#/bundleFormats" ||
    !Array.isArray(currentFormat.candidateAssets) ||
    new Set(currentFormat.candidateAssets).size !== currentFormat.candidateAssets.length ||
    currentFormat.candidateAssets.some(name => !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name)) ||
    JSON.stringify(legacyFormat.candidateAssets) !== JSON.stringify(candidateReceipt?.portableAssets) ||
    !candidateCommands.includes("cloud-portable-assets.ts copy-candidate") ||
    !candidateCommands.includes("cloud-portable-assets.ts checksums") ||
    !sealStep.includes("cloud-portable-assets.ts prepare-release") ||
    !sealStep.includes("cloud-portable-assets.ts checksums") ||
    !publishValidate.includes("cloud-portable-assets.ts validate-release") ||
    !readbackCommands.includes("cloud-portable-assets.ts release-names") ||
    !readbackCommands.includes("cloud-portable-assets.ts validate-release") ||
    (releasePublish?.steps || []).find(step => step.name === "Attest release assets")?.with?.["subject-path"] !== "artifacts/publication/release/*") {
  throw new Error("distribution asset contract must own all bundle producers and consumers");
}
if (candidateContract.schemaVersion !== 2 || candidateReceipt?.schemaVersion !== 2 ||
    JSON.stringify(candidateReceipt?.platforms) !== JSON.stringify(["linux/amd64", "linux/arm64"]) ||
    JSON.stringify(candidateReceipt?.portableAssets) !== JSON.stringify(legacyFormat.candidateAssets) ||
    candidateReceipt?.manifest !== "opl-cloud-candidate.json" ||
    candidateReceipt?.checksumManifest !== "SHA256SUMS") {
  throw new Error("Candidate receipt contract boundary is invalid");
}
if (contract.schemaVersion !== 8 || contract.productRepository !== "gaofeng21cn/one-person-lab-cloud" ||
    contract.instanceHandoff?.repository !== "gaofeng21cn/opl-instance-medopl" ||
    contract.candidate?.formalPublication !== false ||
    contract.candidate?.instanceDeployment !== false ||
    contract.portableInstallation?.providerSelection !== "instance_or_installer_owned" ||
    contract.portableInstallation?.composeScope !== "owner_topology_processes_only" ||
    contract.candidate?.contract !== "packages/contracts/opl-cloud-candidate-receipt-contract.json#candidateReceiptV2" ||
    JSON.stringify(Object.keys(contract.candidate || {}).sort()) !== JSON.stringify([
      "artifactLocator", "contract", "formalPublication", "instanceDeployment", "registry", "workflow"
    ]) ||
    contract.candidate?.artifactLocator?.kind !== "github_actions_artifact" ||
    JSON.stringify(contract.candidate?.artifactLocator?.exactKeys) !== JSON.stringify(["workflowRunId", "workflowRunAttempt", "artifactName"]) ||
    contract.candidate?.artifactLocator?.workflowRunIdField !== "provenance.workflowRunId" ||
    contract.candidate?.artifactLocator?.workflowRunAttemptField !== "provenance.workflowRunAttempt" ||
    contract.candidate?.artifactLocator?.artifactNameTemplate !== "opl-cloud-candidate-{product.sha}-{provenance.workflowRunAttempt}" ||
    contract.candidate?.artifactLocator?.artifactNameProductShaField !== "product.sha" ||
    contract.candidate?.artifactLocator?.artifactNameRunAttemptField !== "provenance.workflowRunAttempt" ||
    contract.distribution?.publication?.promotionMode !== "qualified_candidate_digest_without_rebuild" ||
    JSON.stringify(requiredEvidenceClasses) !== JSON.stringify(["candidate", "localInstallation", "instanceDeployment", "businessSourcePaths"]) ||
    JSON.stringify(requiredEvidenceNames) !== JSON.stringify(["candidate", "local_qualification", "instance_deployment_verified", "business_source_paths_verified"]) ||
    contract.distribution?.releaseManifestSchemaVersion !== 4 ||
    JSON.stringify(contract.distribution?.publication?.businessSourcePaths) !== JSON.stringify({
      owner: contract.productRepository, workflow: ".github/workflows/pull-request-ci.yml", status: "source_verified",
      paidBusinessExecuted: false, customerReadsExecuted: false, binds: ["productSha", "productTree", "ciRun", "entryPathHashes"]
    }) ||
    JSON.stringify(contract.distribution?.publication?.instanceDeployment) !== JSON.stringify({
      owner: contract.instanceHandoff?.repository,
      workflow: ".github/workflows/deployment-verify.yml",
      receiptStatus: "deployment_verified",
      binds: ["receiptDigest", "candidateReceiptDigest", "productSha", "productTree", "cloudImageDigest"]
    }) ||
    contract.distribution?.publication?.instanceEvidenceReader?.secret !== "OPL_GITHUB_RELEASE_ADMIN_TOKEN" ||
    contract.distribution?.publication?.instanceEvidenceReader?.repository !== "gaofeng21cn/opl-instance-medopl" ||
    JSON.stringify(contract.distribution?.publication?.instanceEvidenceReader?.permissions) !== JSON.stringify(["actions:read", "contents:read"]) ||
    contract.distribution?.publication?.serialization?.concurrencyGroup !== "opl-cloud-publication-global" ||
    contract.distribution?.publication?.serialization?.ownerJob !== "publish" ||
    JSON.stringify(contract.distribution?.publication?.serialization?.unlockedJobs) !== JSON.stringify(["admission"]) ||
    contract.distribution?.publication?.recovery?.sameTagAssetReplacement !== true ||
    contract.distribution?.publication?.recovery?.versionBumpRequiredForPublicationFailure !== false ||
    contract.distribution?.publicReadback?.workflow !== ".github/workflows/release-opl-cloud-public-readback.yml" ||
    contract.distribution?.publicReadback?.job !== "public-readback" ||
    contract.distribution?.publicReadback?.trigger !== "workflow_run" ||
    contract.distribution?.publicReadback?.manualRetryInput !== "publication_run_id" ||
    contract.distribution?.publicReadback?.nonBlocking !== true ||
    JSON.stringify(contract.distribution?.exactByteBinding) !== JSON.stringify([
      "releaseTag", "productSha", "imageDigest", "releaseAssetSha256", "releaseAssetProvenance"
    ]) ||
    contract.instanceHandoff?.artifactLocatorContract !== "packages/contracts/opl-cloud-distribution-contract.json#candidate.artifactLocator" ||
    JSON.stringify(contract.instanceHandoff?.inputs) !== JSON.stringify(["candidate_manifest_b64"])) {
  throw new Error("distribution owner boundary is invalid");
}

// The published surface is the owner topology. The distribution contract
// declares which Compose file realizes that topology, which processes it runs
// and that the owner topology contract itself is a Candidate-only asset. The
// assertions below read the owner topology contract and the declared
// entrypoint, so a declaration that drifts from the shipped surface fails the
// boundary instead of shipping.
const installation = contract.portableInstallation || {};
const topology = JSON.parse(await readFile(new URL("deploy/portable/opl-cloud-owner-topology.json", root), "utf8"));
const topologyProcesses = (topology.processes || []).map((process) => process.service);
const ownerTopologyContractAsset = installation.ownerTopology?.contract;
const ownerTopologyComposeAsset = installation.ownerTopology?.compose;
const candidateOnlyAssets = bundleFormats.candidateOnlyAssets || [];
const currentCandidateAssets = assetNames(contract, bundleFormats.current, "candidate");
const currentReleaseAssets = assetNames(contract, bundleFormats.current, "release");
const assetSourcePath = (name) => bundleFormats.assetSources?.[name] ?? `${bundleFormats.defaultAssetSourceDirectory}/${name}`;

if (installation.entrypoint !== ownerTopologyComposeAsset) {
  throw new Error(`distribution contract entrypoint must be the owner topology Compose file: ${installation.entrypoint} != ${ownerTopologyComposeAsset}`);
}
const undeclaredProcesses = (installation.processes || []).filter((name) => !topologyProcesses.includes(name));
if (undeclaredProcesses.length > 0) {
  throw new Error(`distribution contract declares processes the owner topology does not: ${undeclaredProcesses.join(",")}`);
}
if (JSON.stringify(installation.processes) !== JSON.stringify(topologyProcesses)) {
  throw new Error(`distribution contract processes must equal the owner topology processes in order: ${JSON.stringify(installation.processes)} != ${JSON.stringify(topologyProcesses)}`);
}
if (!candidateOnlyAssets.includes(ownerTopologyContractAsset)) {
  throw new Error(`distribution contract must declare the owner topology contract Candidate-only: ${ownerTopologyContractAsset}`);
}
if (typeof currentFormat.releaseAssets === "string") {
  throw new Error(`the current bundle format must declare its release assets explicitly instead of aliasing the candidate assets: ${bundleFormats.current}`);
}
if (!Array.isArray(currentFormat.releaseAssets)) {
  throw new Error(`the current bundle format must declare its release assets as an ordered list: ${bundleFormats.current}`);
}
if (!currentCandidateAssets.includes(installation.entrypoint)) {
  throw new Error(`candidate bundle must carry the declared entrypoint: ${installation.entrypoint}`);
}
if (!currentCandidateAssets.includes(ownerTopologyContractAsset)) {
  throw new Error(`candidate bundle must carry the owner topology contract: ${ownerTopologyContractAsset}`);
}
if (currentReleaseAssets.includes(ownerTopologyContractAsset)) {
  throw new Error(`the owner topology contract must not be published in the Release bundle: ${ownerTopologyContractAsset}`);
}

const entrypointDocument = YAML.parseDocument(await readFile(new URL(assetSourcePath(installation.entrypoint), root), "utf8"), {
  customTags: [
    { tag: "!override", collection: "seq" },
    { tag: "!override", collection: "map" },
    { tag: "!reset", resolve: () => null }
  ],
  logLevel: "silent"
});
if (entrypointDocument.errors.length > 0) {
  throw new Error(`the declared entrypoint is not a readable Compose file: ${installation.entrypoint}`);
}
const entrypointModel = entrypointDocument.toJS();
const entrypointServices = entrypointModel.services || {};
const referencedFiles = new Set();
for (const service of Object.values(entrypointServices)) {
  const extended = service?.extends?.file;
  if (typeof extended === "string") referencedFiles.add(extended.replace(/^\.\//, "").split("/").pop());
}
for (const config of Object.values(entrypointModel.configs || {})) {
  if (typeof config?.file === "string") referencedFiles.add(config.file.replace(/^\.\//, "").split("/").pop());
}
for (const name of referencedFiles) {
  if (!currentCandidateAssets.includes(name)) {
    throw new Error(`candidate bundle must carry every file the entrypoint references: ${name}`);
  }
}
for (const process of topology.processes || []) {
  if (!entrypointServices[process.service] && !entrypointServices[process.ownerName]) {
    throw new Error(`the published Compose surface must run every owner process: ${process.service}`);
  }
}

// Every format publishes exactly its candidate assets minus the assets the
// contract declares Candidate-only. The alias that used to publish the whole
// candidate list would publish the owner topology contract as well.
for (const format of Object.keys(bundleFormats.formats || {})) {
  const candidate = assetNames(contract, format, "candidate");
  const release = assetNames(contract, format, "release");
  for (const name of candidateOnlyAssets) {
    if (!candidate.includes(name)) {
      throw new Error(`candidate-only asset must stay in the Candidate bundle: ${format}:${name}`);
    }
    if (release.includes(name)) {
      throw new Error(`candidate-only asset must not be published in the Release bundle: ${format}:${name}`);
    }
  }
  const expectedRelease = candidate.filter((name) => !candidateOnlyAssets.includes(name));
  if (JSON.stringify(release) !== JSON.stringify(expectedRelease)) {
    throw new Error(`release asset set must be the candidate assets minus the candidate-only assets: ${format}`);
  }
}

console.log("OPL Cloud product distribution boundary is valid");

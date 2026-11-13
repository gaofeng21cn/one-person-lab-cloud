import { createHash } from "node:crypto";
import { chmod, readFile, writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

import { readFileSync } from "node:fs";
import { assetNames, canonicalJson, parseStrictJson, readInstallationBundle, validateCandidate } from "../deploy/portable/installer-manifest.mjs";
export { canonicalJson } from "../deploy/portable/installer-manifest.mjs";
export const distributionContract = JSON.parse(readFileSync(new URL("../packages/contracts/opl-cloud-distribution-contract.json", import.meta.url), "utf8"));
export const PORTABLE_ASSETS: string[] = assetNames(distributionContract, distributionContract.bundleFormats.current, "candidate");
function sha256(value: string | Buffer) { return createHash("sha256").update(value).digest("hex"); }
export function validateCloudCandidateReceipt(value: unknown): Record<string, any> {
  try { return validateCandidate(value, distributionContract); }
  catch { throw new Error("cloud_candidate_receipt_invalid"); }
}

export function candidateReceiptDigest(value: unknown): string {
  const receipt = validateCloudCandidateReceipt(value);
  return `sha256:${sha256(canonicalJson(receipt))}`;
}

export function decodeCandidateReceipt(encoded: string): Record<string, unknown> {
  if (typeof encoded !== "string" || encoded.length === 0 ||
      !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) {
    throw new Error("cloud_candidate_receipt_encoding_invalid");
  }
  const decoded = Buffer.from(encoded, "base64");
  if (decoded.toString("base64") !== encoded) throw new Error("cloud_candidate_receipt_encoding_invalid");
  try {
    return validateCloudCandidateReceipt(parseStrictJson(decoded));
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("cloud_candidate_receipt_")) throw error;
    throw new Error("cloud_candidate_receipt_encoding_invalid");
  }
}

export async function writeCandidateReceipt(path: string, value: unknown) {
  if (!path) throw new Error("cloud_candidate_receipt_output_invalid");
  const receipt = validateCloudCandidateReceipt(value);
  await writeFile(path, `${canonicalJson(receipt)}\n`, { mode: 0o600 });
  await chmod(path, 0o600);
}

async function readReceipt(path: string) {
  try {
    return validateCloudCandidateReceipt(parseStrictJson(await readFile(path)));
  } catch (error) {
    if (error instanceof Error && error.message.startsWith("cloud_candidate_receipt_")) throw error;
    throw new Error("cloud_candidate_receipt_invalid");
  }
}

export async function validateCandidateBundle(directory: string): Promise<Record<string, any>> {
  try {
    const result = await readInstallationBundle(directory, distributionContract);
    if (result.manifest.kind !== "opl_cloud_candidate") throw new Error("wrong_manifest");
    return result.manifest;
  } catch { throw new Error("cloud_candidate_bundle_invalid"); }
}

// Shared Candidate identity carried by native installation evidence. Callers
// retain their own receipt-version, run-authority and qualification decisions.
export function candidateInstallationIdentity(value: unknown) {
  const candidate = validateCloudCandidateReceipt(value);
  const product = candidate.product as Record<string, unknown>;
  const image = candidate.cloudImage as Record<string, unknown>;
  const platforms = image.platforms as Array<Record<string, unknown>>;
  return {
    candidateReceiptDigest: candidateReceiptDigest(candidate),
    productSha: String(product.sha), productTree: String(product.tree),
    cloudImageRef: String(image.indexRef), cloudImageDigest: String(image.indexDigest),
    cloudImageRevision: String(image.revision),
    cloudRuntimeChildDigest: String(platforms.find(entry => entry.platform === "linux/amd64")!.digest)
  };
}

function option(args: string[], name: string) {
  const indexes = args.flatMap((value, index) => value === name ? [index] : []);
  if (indexes.length !== 1 || !args[indexes[0] + 1] || args[indexes[0] + 1].startsWith("--")) {
    throw new Error(`${name.slice(2).replaceAll("-", "_")}_invalid`);
  }
  return args[indexes[0] + 1];
}

export async function main(args = process.argv.slice(2)) {
  const [command] = args;
  if (command === "validate-bundle") return validateCandidateBundle(option(args, "--bundle"));

  const receipt = await readReceipt(option(args, "--receipt"));
  if (command === "validate") return receipt;
  if (command === "digest") {
    process.stdout.write(`${candidateReceiptDigest(receipt)}\n`);
    return receipt;
  }
  if (command === "export-env") {
    const githubEnv = option(args, "--github-env");
    const product = receipt.product as Record<string, unknown>;
    const cloudImage = receipt.cloudImage as Record<string, unknown>;
    const platforms = cloudImage.platforms as Array<Record<string, unknown>>;
    await writeFile(githubEnv, [
      `OPL_CANDIDATE_RECEIPT_DIGEST=${candidateReceiptDigest(receipt)}`,
      `OPL_PRODUCT_SHA=${product.sha}`,
      `OPL_PRODUCT_TREE=${product.tree}`,
      `OPL_CLOUD_IMAGE=${cloudImage.indexRef}`,
      `OPL_CLOUD_IMAGE_DIGEST=${cloudImage.indexDigest}`,
      `OPL_CLOUD_IMAGE_AMD64_DIGEST=${platforms[0].digest}`,
      `OPL_CLOUD_IMAGE_ARM64_DIGEST=${platforms[1].digest}`,
      ""
    ].join("\n"), { flag: "a" });
    return receipt;
  }
  throw new Error("cloud_candidate_receipt_command_invalid");
}

if (process.argv[1]?.endsWith("cloud-candidate-receipt.ts")) {
  main().catch((error) => {
    console.error(error instanceof Error ? error.message : "cloud_candidate_receipt_invalid");
    process.exit(1);
  });
}

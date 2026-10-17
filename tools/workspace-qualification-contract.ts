// The provider-neutral qualification contract.
//
// Before this module, provider identity was asserted as a literal inside the
// J0 validator (`value.kind !== "opl.local-workspace.j0-ready.v1"` and
// `value.provider !== "local-docker"`), so no provider other than Local-Docker
// could ever produce a qualifying receipt and the schema itself named one
// provider. A Tencent-TKE adapter therefore had no seam to reuse and would have
// had to copy the validator.
//
// Here the provider is declared by the caller as data. The contract knows the
// shape of a qualification receipt and nothing about which provider runs it.

// Named for the qualification, not for any provider.
export const QUALIFICATION_J0_READY_KIND = "opl.workspace-qualification.j0-ready.v2";

// The J0 producer lives outside this repository and still emits the old,
// provider-named kind. It stays acceptable to any profile that declares it, so
// moving the in-repo schema to the neutral kind breaks no live path.
export const LEGACY_QUALIFICATION_J0_READY_KINDS = ["opl.local-workspace.j0-ready.v1"];

const J0_READY_KEYS = ["schemaVersion", "kind", "status", "source", "authority", "provider", "failed", "skipped"];

export function hasExactKeys(value, keys) {
  return value && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

export function j0ReadyReceiptError() {
  const error = new Error("j0_ready_receipt_invalid");
  error.code = "j0_ready_receipt_invalid";
  return error;
}

export function qualificationProviderProfile(provider, { legacyReceiptKinds = [] } = {}) {
  const name = String(provider || "").trim();
  if (!name) throw new Error("qualification provider profile requires a provider identity");
  return Object.freeze({
    provider: name,
    receiptKinds: Object.freeze([QUALIFICATION_J0_READY_KIND, ...legacyReceiptKinds])
  });
}

export function createJ0ReadyReceiptValidator(profile) {
  const { provider, receiptKinds } = qualificationProviderProfile(profile?.provider, {
    legacyReceiptKinds: (profile?.receiptKinds || []).filter((kind) => kind !== QUALIFICATION_J0_READY_KIND)
  });
  return function validateJ0ReadyReceiptForProfile(value, sourceSha, sourceTree) {
    if (!hasExactKeys(value, J0_READY_KEYS) ||
      value.schemaVersion !== 1 || !receiptKinds.includes(value.kind) || value.status !== "READY" ||
      !hasExactKeys(value.source, ["sha", "tree", "clean"]) ||
      value.source?.sha !== sourceSha || value.source?.tree !== sourceTree || value.source?.clean !== true ||
      value.failed !== 0 || value.skipped !== 0 || value.provider !== provider ||
      !hasExactKeys(value.authority, ["mode", "class", "dedicated", "confirmed"]) ||
      value.authority?.mode !== "live" || value.authority?.class !== "sandbox" ||
      value.authority?.dedicated !== true || value.authority?.confirmed !== true) {
      throw j0ReadyReceiptError();
    }
    return value;
  };
}
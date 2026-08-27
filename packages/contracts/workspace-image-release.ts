export interface WorkspaceImageRelease {
  readonly version: string;
  readonly image: string;
}

export interface WorkspaceImageReleaseCatalog {
  readonly schemaVersion: 1;
  readonly releases: readonly WorkspaceImageRelease[];
}

const digestPattern = /^sha256:[a-f0-9]{64}$/;
const versionPattern = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/;

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const keys = Object.keys(value).sort();
  return keys.length === expected.length && keys.every((key, index) => key === [...expected].sort()[index]);
}

export function validWorkspaceImageReference(value: string): boolean {
  const normalized = value.trim();
  const separator = normalized.indexOf("@");
  if (separator <= 0 || normalized.indexOf("@", separator + 1) !== -1) return false;
  return digestPattern.test(normalized.slice(separator + 1));
}

export function decodeWorkspaceImageReleaseCatalog(raw: string, installedImage: string): WorkspaceImageReleaseCatalog | null {
  const installed = installedImage.trim();
  if (!validWorkspaceImageReference(installed)) return null;
  if (!raw.trim()) {
    return { schemaVersion: 1, releases: [{ version: "installed", image: installed }] };
  }

  let decoded: unknown;
  try {
    decoded = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!isRecord(decoded) || !hasExactKeys(decoded, ["releases", "schemaVersion"]) || decoded.schemaVersion !== 1 ||
    !Array.isArray(decoded.releases) || decoded.releases.length < 1 || decoded.releases.length > 32) return null;

  const releases: WorkspaceImageRelease[] = [];
  const versions = new Set<string>();
  const images = new Set<string>();
  for (const item of decoded.releases) {
    if (!isRecord(item) || !hasExactKeys(item, ["image", "version"]) || typeof item.version !== "string" || typeof item.image !== "string") return null;
    const version = item.version.trim();
    const image = item.image.trim();
    if (!versionPattern.test(version) || !validWorkspaceImageReference(image) || versions.has(version) || images.has(image)) return null;
    versions.add(version);
    images.add(image);
    releases.push({ version, image });
  }
  return images.has(installed) ? { schemaVersion: 1, releases } : null;
}

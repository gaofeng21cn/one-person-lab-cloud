export function canonicalJson(value: unknown): string;
export function parseStrictJson(bytes: string | Uint8Array): any;
export function sha256(bytes: Uint8Array | string): string;
export function assetNames(contract: any, format: string, kind: 'candidate' | 'release'): string[];
export function selectAssetFormat(names: string[], contract: any, kind: 'candidate' | 'release'): string;
export function releaseAssetNames(manifest: any, contract: any): string[];
export function validateCandidate(value: any, contract: any): any;
export function validateRelease(value: any, contract: any): any;
export function readInstallationBundle(bundleDir: string, suppliedContract?: any): Promise<{
  manifest: any;
  imageRef: string;
  sourceSha: string;
  sourceTree: string;
  assets: Array<{name: string; sha256: string}>;
}>;

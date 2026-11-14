import { cp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
import { assetNames, canonicalJson, parseStrictJson, readInstallationBundle, releaseAssetNames, selectAssetFormat, sha256, validateRelease } from '../deploy/portable/installer-manifest.mjs';
import { distributionContract as contract, validateCandidateBundle, validateCloudCandidateReceipt } from './cloud-candidate-receipt.ts';

export function releasePayload(candidate: unknown): string[] {
  const receipt = validateCloudCandidateReceipt(candidate);
  const format = selectAssetFormat(receipt.assets.map((a: any) => a.name), contract, 'candidate');
  return assetNames(contract, format, 'release');
}
export async function copyCandidateAssets(source: string, bundle: string) {
  await mkdir(bundle, { recursive: true });
  const assets = [];
  for (const name of assetNames(contract, contract.bundleFormats.current, 'candidate')) {
    const relative = contract.bundleFormats.assetSources[name] ?? `${contract.bundleFormats.defaultAssetSourceDirectory}/${name}`;
    await cp(join(source, relative), join(bundle, name), { dereference: false });
    assets.push({ name, sha256: sha256(await readFile(join(bundle, name))) });
  }
  return assets;
}
export async function prepareRelease(candidateDirectory: string, releaseDirectory: string) {
  const receipt = await validateCandidateBundle(candidateDirectory);
  await mkdir(releaseDirectory, { recursive: true });
  const names = releasePayload(receipt);
  for (const name of names) await cp(join(candidateDirectory, name), join(releaseDirectory, name), { dereference: false });
  const format = selectAssetFormat(receipt.assets.map((a: any) => a.name), contract, 'candidate');
  // The Release publishes the candidate assets minus the contract-declared
  // candidate-only assets, so its digests cover exactly the published assets.
  const digests = names.map((name) => receipt.assets.find((asset: any) => asset.name === name));
  return {
    assets: [...names, contract.bundleFormats.checksumManifest],
    ...(format === 'legacy-v2' ? {} : { assetFormat: format, assetDigests: digests })
  };
}
export async function writeBundleChecksums(directory: string, kind: 'candidate' | 'release') {
  const manifestName = contract.bundleFormats[`${kind}Manifest`];
  const manifest = parseStrictJson(await readFile(join(directory, manifestName)));
  const names = kind === 'candidate' ? validateCloudCandidateReceipt(manifest).assets.map((a: any) => a.name) :
    releaseAssetNames(manifest, contract).slice(0, -2);
  const sums = [];
  for (const name of [...names, manifestName]) sums.push(`${sha256(await readFile(join(directory, name)))}  ${name}`);
  await writeFile(join(directory, contract.bundleFormats.checksumManifest), `${sums.join('\n')}\n`);
  await readInstallationBundle(directory, contract);
}
export async function main(args = process.argv.slice(2)) {
  const [command, first, second] = args;
  if (command === 'copy-candidate') {
    process.stdout.write(`${canonicalJson(await copyCandidateAssets(first, second))}\n`);
  } else if (command === 'prepare-release') {
    process.stdout.write(`${canonicalJson(await prepareRelease(first, second))}\n`);
  } else if (command === 'checksums') {
    if (second !== 'candidate' && second !== 'release') throw new Error('bundle_kind_invalid');
    await writeBundleChecksums(first, second);
  } else if (command === 'release-names') {
    const manifest = validateRelease(parseStrictJson(await readFile(first)), contract);
    process.stdout.write(`${releaseAssetNames(manifest, contract).join('\n')}\n`);
  } else if (command === 'validate-release') {
    const result = await readInstallationBundle(first, contract);
    if (result.manifest.kind) throw new Error('release_bundle_invalid');
  } else throw new Error('portable_asset_command_invalid');
}
if (process.argv[1]?.endsWith("cloud-portable-assets.ts")) {
  main().catch(error => { console.error(error.message); process.exitCode = 1; });
}

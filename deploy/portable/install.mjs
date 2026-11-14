#!/usr/bin/env node
/**
 * Install or update one OPL Cloud bundle. The bundle is immutable input;
 * installation state lives outside it and keeps configuration, credentials,
 * certificates and data across updates.
 */
import { createHash, randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { mkdir, readFile, writeFile, chmod, rename, access, copyFile } from 'node:fs/promises';
import { dirname, isAbsolute, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readInstallationBundle, parseStrictJson, sha256 } from './installer-manifest.mjs';

const MIN_SECRET = 32;
const PACKAGE_SCHEMA_DIGEST = 'sha256:0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883';
const PUBLISHER_SCHEMA_DIGEST = 'sha256:176d492a0c0c491ca4e180d9e5d3e04a08ab9c621c4254806658ef529c77b5ef';
const OWNER_DATABASE_SCRIPT = 'owner-databases.sh';
const POSTGRES_READY_TIMEOUT_MS = 120000;
const POSTGRES_READY_INTERVAL_MS = 1000;
const fail = (message, code = 1) => { throw Object.assign(new Error(message), { exitCode: code }); };
const immutable = new Set(['OPL_CLOUD_IMAGE', 'OPL_RELEASE_SHA', 'OPL_RELEASE_TREE']);

function usage() {
  return `Usage: bash install.sh [--install-dir PATH] [--dry-run]\n\n` +
    'The installation directory is separate from the downloaded bundle.\n';
}
function parseArgs(args) {
  const result = { installDir: resolve(process.cwd(), 'opl-cloud'), dryRun: false };
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--help' || arg === '-h') { console.log(usage()); process.exit(0); }
    if (arg === '--dry-run') { result.dryRun = true; continue; }
    if (arg === '--install-dir') {
      if (!args[i + 1] || args[i + 1].startsWith('--')) fail('--install-dir requires a path', 2);
      result.installDir = resolve(args[++i]); continue;
    }
    fail(`unknown argument: ${arg}`, 2);
  }
  return result;
}
function sha256File(bytes) { return `sha256:${createHash('sha256').update(bytes).digest('hex')}`; }
function strictEnvLine(name, value) {
  const text = String(value);
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) || /[\r\n\0]/.test(text)) fail(`installation_value_invalid:${name}`);
  return text;
}
function isPlaceholder(value) { return /<[^>\n]+>/.test(String(value ?? '')); }
function isSecretName(name) { return /(?:PASSWORD|TOKEN|KEY|SEED)$/.test(name); }
function randomSecret() { return randomBytes(32).toString('hex'); }
function parseTemplate(text) {
  const values = new Map();
  for (const line of text.split('\n')) {
    const match = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line);
    if (!match) continue;
    if (values.has(match[1])) fail(`template_duplicate:${match[1]}`);
    values.set(match[1], match[2]);
  }
  return values;
}
function cleanEnvironment() {
  const env = {};
  for (const [name, value] of Object.entries(process.env)) {
    if (/^(OPL_|COMPOSE_)/.test(name)) continue;
    if (value !== undefined) env[name] = value;
  }
  env.PATH = process.env.PATH || '/usr/bin:/bin';
  if (process.env.DOCKER_HOST) env.DOCKER_HOST = process.env.DOCKER_HOST;
  if (process.env.DOCKER_CONTEXT) env.DOCKER_CONTEXT = process.env.DOCKER_CONTEXT;
  return env;
}
function compose(bundle, env, args, input) {
  return execFileSync('docker', ['compose', '--env-file', '/dev/null', '-p', 'opl-cloud', ...args], {
    cwd: bundle, env, encoding: 'utf8', input,
    stdio: [input === undefined ? 'ignore' : 'pipe', 'pipe', 'pipe']
  });
}
async function requiredVariables(bundle) {
  const env = cleanEnvironment();
  const raw = compose(bundle, env, ['-f', 'compose.cloud.yaml', 'config', '--variables', '--format', 'json']);
  let model;
  try { model = JSON.parse(raw); } catch { fail('compose_variables_invalid'); }
  const required = new Set(Object.values(model).filter(item => item.Required || (item.DefaultValue === '' && item.PresenceValue === '')).map(item => item.Name));
  for (const file of ['compose.cloud.yaml']) {
    const text = await readFile(join(bundle, file), 'utf8');
    for (const match of text.matchAll(/\$\{([A-Za-z_][A-Za-z0-9_]*):[?+]/g)) required.add(match[1]);
  }
  for (const name of [...required]) if (name.startsWith('OPL_QUALIFICATION_') || ['OPL_WORKSPACE_APPLICATION_DOMAIN', 'OPL_WORKSPACE_APPLICATION_INGRESS_PEERS'].includes(name)) required.delete(name);
  const result = [...required].sort();
  if (!result.length) fail('compose_has_no_required_variables');
  return result;
}
function targetTokenVariable(peerName) {
  const owner = peerName.slice('OPL_'.length, -'_PEER_TOKENS'.length).toLowerCase();
  return owner === 'tenant' ? 'OPL_CLOUD_IDENTITY_TOKEN' : `OPL_${owner.toUpperCase()}_TOKEN`;
}
function derivePeerToken(name, template, values) {
  let shape;
  try { shape = JSON.parse(template); } catch { fail(`peer_tokens_template_invalid:${name}`); }
  if (!shape || Array.isArray(shape) || typeof shape !== 'object') fail(`peer_tokens_template_invalid:${name}`);
  const token = values.get(targetTokenVariable(name));
  if (!token || token.length < MIN_SECRET) fail(`peer_token_missing:${name}`);
  for (const key of Object.keys(shape)) shape[key] = token;
  return JSON.stringify(shape);
}
function existingValues(state) {
  const values =
    state?.values && typeof state.values === "object"
      ? new Map(Object.entries(state.values))
      : new Map();
  // Migrate the persisted installation, not just the template. Delete the old
  // spelling after one successful save so there is still only one fact owner.
  const renamed = {
    OPL_LOCAL_V2_SUBNET: "OPL_DOCKER_SUBNET",
    OPL_LOCAL_V2_POSTGRES_HOST: "OPL_POSTGRES_HOST",
    OPL_LOCAL_V2_PROXY_PORT: "OPL_PROXY_PORT",
    OPL_LOCAL_V2_GATEWAY_CONTAINER: "OPL_FABRIC_LOCAL_DOCKER_GATEWAY_CONTAINER",
    OPL_LOCAL_V2_PUBLIC_URL: "OPL_PUBLIC_URL",
    OPL_LOCAL_V2_SUB2API_URL: "OPL_SUB2API_URL",
    OPL_LOCAL_V2_CAPABILITY_OBJECT_URL: "OPL_CAPABILITY_OBJECT_URL",
    OPL_LOCAL_V2_CAPABILITY_OBJECT_INTERNAL_URL:
      "OPL_CAPABILITY_OBJECT_INTERNAL_URL",
    OPL_LOCAL_V2_PKI_DIR: "OPL_PKI_DIR",
    OPL_LOCAL_V2_PACKAGE_SCHEMA_PATH: "OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH",
    OPL_LOCAL_V2_PACKAGE_SCHEMA_DIGEST: "OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST",
    OPL_LOCAL_V2_PUBLISHER_SCHEMA_DIGEST: "OPL_PUBLISHER_SCHEMA_DIGEST",
  };
  const filled = (value) =>
    typeof value === "string" &&
    value !== "" &&
    !isPlaceholder(value) &&
    !value.startsWith("unset-");
  for (const [oldName, name] of Object.entries(renamed)) {
    if (filled(values.get(oldName))) {
      if (filled(values.get(name)) && values.get(name) !== values.get(oldName))
        fail(`installation_fact_conflict:${oldName}:${name}`);
      values.set(name, values.get(oldName));
    }
    values.delete(oldName);
  }
  return values;
}
async function readState(path) {
  try { return parseStrictJson(await readFile(path)); } catch (error) {
    if (error.code === 'ENOENT') return null;
    fail('installation_state_invalid');
  }
}
async function atomicJson(path, value) {
  const temp = `${path}.tmp-${process.pid}`;
  await writeFile(temp, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  await rename(temp, path); await chmod(path, 0o600);
}
function requireAbsolute(name, value) { if (!isAbsolute(value)) fail(`installation_path_must_be_absolute:${name}`); }
async function ensureSchema(values, installDir, bundle) {
  const configured = values.get('OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH');
  if (!configured || isPlaceholder(configured) || configured.startsWith('unset-')) fail('OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH is required');
  requireAbsolute('OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH', configured);
  try { const digest = sha256File(await readFile(configured)); if (digest !== PACKAGE_SCHEMA_DIGEST) fail('package_schema_digest_mismatch'); }
  catch (error) { if (error.message === 'package_schema_digest_mismatch') throw error; fail('package_schema_unreadable'); }
  values.set('OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST', PACKAGE_SCHEMA_DIGEST);
  values.set('OPL_PUBLISHER_SCHEMA_DIGEST', PUBLISHER_SCHEMA_DIGEST);
}
function runtimeImageReadback(bundle, env, expected) {
  const config = JSON.parse(compose(bundle, env, ['-f', 'compose.cloud.yaml', 'config', '--format', 'json']));
  const productServices = Object.entries(config.services || {}).filter(([name, service]) => !['postgres', 'cloud-local-v2'].includes(name) && service.image);
  for (const [name, service] of productServices) {
    const id = compose(bundle, env, ['-f', 'compose.cloud.yaml', 'ps', '-q', name]).trim();
    if (!id) fail(`service_container_missing:${name}`);
    const image = execFileSync('docker', ['inspect', '--format', '{{.Config.Image}}', id], { env, encoding: 'utf8' }).trim();
    if (image !== expected) fail(`service_image_mismatch:${name}`);
  }
}
function composeServices(text) {
  try {
    const trimmed = text.trim();
    const services = trimmed.startsWith('[') ? JSON.parse(trimmed) : trimmed.split(/\r?\n/).filter(Boolean).map(line => JSON.parse(line));
    if (!Array.isArray(services) || services.some(service => !service || typeof service !== 'object' || Array.isArray(service))) fail('cloud_services_invalid');
    return services;
  } catch { fail('cloud_services_invalid'); }
}
function ownerReadback(bundle, env, sourceSha) {
  const probes = [
    ['tenant', 'tenant', '127.0.0.1:8187'], ['resource-catalog', 'resource_catalog', '127.0.0.1:8186'],
    ['ledger', 'ledger', '127.0.0.1:8189'], ['capability', 'capability', '127.0.0.1:8181'],
    ['build', 'build', '127.0.0.1:8182'], ['runtime-control', 'runtime_control', '127.0.0.1:8183'],
    ['workspace', 'workspace', '127.0.0.1:8184'], ['serve', 'serve', '127.0.0.1:8185'],
    ['fabric', 'fabric', '127.0.0.1:8188'], ['console-bff', 'console_bff', '127.0.0.1:8190'],
    ['control-plane', 'control_plane', 'http://127.0.0.1:8787']
  ];
  for (const [service, owner, address] of probes) {
    const args = ['-f', 'compose.cloud.yaml', 'exec', '-T', service, '/usr/local/bin/opl-owner-probe', '--owner', owner, '--addr', address, '--timeout', '10s'];
    if (owner === 'control_plane') args.push('--expected-source-sha', sourceSha);
    let output;
    try { output = compose(bundle, env, args).trim(); } catch (error) { fail(`owner_readback_failed:${owner}:${error.message}`); }
    let result; try { result = JSON.parse(output); } catch { fail(`owner_readback_invalid:${owner}`); }
    if (result.status !== 'ok') fail(`owner_not_ready:${owner}:${result.errorCode || 'unknown'}`);
  }
}

const sleep = (ms) => new Promise(resolve => setTimeout(resolve, ms));
function boundedMilliseconds(value, fallback) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}
// PostgreSQL only answers on its TCP socket once the image finished its own
// initialization. The socket-only temporary server that runs
// /docker-entrypoint-initdb.d is therefore never mistaken for a ready server,
// and the wait cannot deadlock on the health check that needs the databases
// this installation is about to create.
function postgresReadyCommand() {
  return ['-f', 'compose.cloud.yaml', 'exec', '-T', 'postgres', 'sh', '-c',
    'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atqc "select 1"'];
}
async function waitForPostgres(bundle, env) {
  const timeoutMs = boundedMilliseconds(process.env.OPL_INSTALL_POSTGRES_READY_TIMEOUT_MS, POSTGRES_READY_TIMEOUT_MS);
  const intervalMs = boundedMilliseconds(process.env.OPL_INSTALL_POSTGRES_READY_INTERVAL_MS, POSTGRES_READY_INTERVAL_MS);
  const deadline = Date.now() + timeoutMs;
  for (let attempt = 1; ; attempt++) {
    try { compose(bundle, env, postgresReadyCommand()); return attempt; }
    catch {
      if (Date.now() >= deadline) fail(`postgres_not_ready:${attempt}`);
      await sleep(intervalMs);
    }
  }
}
// The PostgreSQL image runs /docker-entrypoint-initdb.d only against an empty
// data directory, so an update over an existing volume would start the owners
// without their databases. The bundle carries the same idempotent script the
// image runs, and this installation runs it explicitly over standard input,
// so the script travels with the bundle instead of a container path.
async function provisionOwnerDatabases(bundle, env) {
  let script;
  try { script = await readFile(join(bundle, OWNER_DATABASE_SCRIPT), 'utf8'); }
  catch { fail('owner_database_script_missing'); }
  try { compose(bundle, env, ['-f', 'compose.cloud.yaml', 'exec', '-T', 'postgres', 'sh', '-s'], script); }
  catch { fail('owner_database_provision_failed'); }
}

function validateValues(required, values) {
  const missing = required.filter(name => {
    const value = values.get(name); return value === undefined || value === '' || isPlaceholder(value) || String(value).startsWith('unset-');
  });
  if (missing.length) fail(`installation_facts_required:\n${missing.join('\n')}`, 2);
  for (const name of required) strictEnvLine(name, values.get(name));
  const passwords = [...values.entries()].filter(([name]) => name === 'OPL_POSTGRES_ADMIN_PASSWORD' || name.endsWith('_DATABASE_PASSWORD'));
  for (const [name, value] of passwords) if (value.length < MIN_SECRET) fail(`database_secret_too_short:${name}`);
  if (new Set(passwords.map(([, value]) => value)).size !== passwords.length) fail('database_secrets_must_be_distinct');
}
async function main(args = process.argv.slice(2)) {
  const options = parseArgs(args);
  const bundle = fileURLToPath(new URL('.', import.meta.url));
  const contractPath = join(bundle, 'opl-cloud-distribution-contract.json');
  await access(contractPath).catch(() => fail('bundle_contract_missing'));
  const bundleInfo = await readInstallationBundle(bundle);
  const statePath = join(options.installDir, 'installation.json');
  const state = await readState(statePath);
  if (state && state.schemaVersion !== 1) fail('installation_state_version_unsupported');
  if (state && state.imageRef === bundleInfo.imageRef && state.sourceSha !== bundleInfo.sourceSha) fail('installation_identity_conflict');
  await mkdir(options.installDir, { recursive: true, mode: 0o700 });
  const defaultPkiDir = join(options.installDir, 'pki');
  const dataDir = join(options.installDir, 'data');
  await mkdir(dataDir, { recursive: true, mode: 0o700 });
  const template = parseTemplate(await readFile(join(bundle, 'opl-cloud.env.example'), 'utf8'));
  const required = await requiredVariables(bundle);
  const values = existingValues(state);
  for (const name of required) {
    if (immutable.has(name)) continue;
    if (values.has(name) && !isPlaceholder(values.get(name)) && !String(values.get(name)).startsWith('unset-')) continue;
    const templateValue = template.get(name) ?? '';
    if (name === 'OPL_PKI_DIR') values.set(name, defaultPkiDir);
    else if (name === 'OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH') values.set(name, join(options.installDir, 'schemas', 'agent-plugins-1.0.0-plugin.schema.json'));
    else if (name === 'OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT') values.set(name, join(dataDir, 'fabric-storage'));
    else if (name === 'OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT') values.set(name, join(dataDir, 'fabric-secrets'));
    else if (name === 'OPL_POSTGRES_DATA_ROOT') values.set(name, join(dataDir, 'postgres'));
    else if (name === 'OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST') values.set(name, PACKAGE_SCHEMA_DIGEST);
    else if (name === 'OPL_PUBLISHER_SCHEMA_DIGEST') values.set(name, PUBLISHER_SCHEMA_DIGEST);
    else if (name.endsWith('_PEER_TOKENS')) values.set(name, `unset-${name.toLowerCase()}`);
    else if (name === 'OPL_CLOUD_IMAGE') values.set(name, bundleInfo.imageRef);
    else if (name === 'OPL_RELEASE_SHA' || name === 'OPL_RELEASE_TREE') values.set(name, name.endsWith('TREE') ? bundleInfo.sourceTree : bundleInfo.sourceSha);
    else if (isSecretName(name)) values.set(name, randomSecret());
    else if (templateValue && !isPlaceholder(templateValue)) values.set(name, templateValue);
    else values.set(name, `unset-${name.toLowerCase()}`);
  }
  values.set('OPL_CLOUD_IMAGE', bundleInfo.imageRef);
  if (template.has('OPL_RELEASE_SHA')) values.set('OPL_RELEASE_SHA', bundleInfo.sourceSha);
  if (template.has('OPL_RELEASE_TREE')) values.set('OPL_RELEASE_TREE', bundleInfo.sourceTree);
  for (const name of required.filter(name => name.endsWith('_PEER_TOKENS'))) values.set(name, derivePeerToken(name, template.get(name), values));
  const schemaPath = values.get('OPL_CAPABILITY_PACKAGE_SCHEMA_HOST_PATH');
  await mkdir(dirname(schemaPath), { recursive: true, mode: 0o700 });
  try { await access(schemaPath); } catch { await copyFile(join(bundle, 'agent-plugins-1.0.0-plugin.schema.json'), schemaPath); await chmod(schemaPath, 0o600); }
  const next = { schemaVersion: 1, project: 'opl-cloud', imageRef: bundleInfo.imageRef, sourceSha: bundleInfo.sourceSha, sourceTree: bundleInfo.sourceTree, values: Object.fromEntries(values), updatedAt: new Date().toISOString() };
  await atomicJson(statePath, next);
  await ensureSchema(values, options.installDir, bundle);
  validateValues(required, values);
  const env = cleanEnvironment();
  // Optional Compose values (subnet, database host and proxy port) are also
  // operator facts; exporting only required values silently replaces them.
  for (const name of new Set([...required, ...template.keys()])) if (values.has(name)) env[name] = strictEnvLine(name, values.get(name));
  const pkiDir = values.get('OPL_PKI_DIR');
  requireAbsolute('OPL_PKI_DIR', pkiDir);
  await mkdir(pkiDir, { recursive: true, mode: 0o700 });
  const pkiNames = ['ca.crt', 'ca.key', 'proxy.crt', 'proxy.key', ...['tenant', 'capability', 'build', 'runtime_control', 'workspace', 'serve', 'resource_catalog', 'ledger', 'fabric', 'console_bff', 'control_plane'].flatMap(name => [`${name}.crt`, `${name}.key`])];
  const pkiPresent = [];
  for (const name of pkiNames) { try { await access(join(pkiDir, name)); pkiPresent.push(name); } catch {} }
  if (pkiPresent.length && pkiPresent.length !== pkiNames.length) fail('installation_pki_incomplete');
  if (!pkiPresent.length) execFileSync('bash', [join(bundle, 'local-v2-pki.sh'), pkiDir, values.get('OPL_WORKSPACE_DOMAIN') || ''], { cwd: bundle, env, stdio: 'inherit' });
  for (const name of ['OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT', 'OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT', 'OPL_POSTGRES_DATA_ROOT']) {
    const path = values.get(name); if (path) await mkdir(path, { recursive: true, mode: 0o700 });
  }
  next.updatedAt = new Date().toISOString();
  await atomicJson(statePath, next);
  const config = JSON.parse(compose(bundle, env, ['-f', 'compose.cloud.yaml', 'config', '--format', 'json']));
  if (options.dryRun) { console.log(`install.mjs: dry run verified ${bundleInfo.imageRef}`); return; }
  compose(bundle, env, ['-f', 'compose.cloud.yaml', 'pull']);
  // Start PostgreSQL on its own, wait until it accepts connections and create
  // the owner databases from the bundle script before the rest of the topology
  // starts. The script is idempotent, so a first installation and an update
  // over an existing data directory take the same path.
  compose(bundle, env, ['-f', 'compose.cloud.yaml', 'up', '-d', 'postgres']);
  await waitForPostgres(bundle, env);
  await provisionOwnerDatabases(bundle, env);
  compose(bundle, env, ['-f', 'compose.cloud.yaml', 'up', '-d', '--wait', '--wait-timeout', '180']);
  const services = composeServices(compose(bundle, env, ['-f', 'compose.cloud.yaml', 'ps', '--format', 'json']));
  const running = new Set(services.filter(service => service.State === 'running').map(service => service.Service));
  if (!services.length || services.some(service => service.State !== 'running') || Object.keys(config.services || {}).some(name => !running.has(name))) fail('cloud_services_not_running');
  runtimeImageReadback(bundle, env, bundleInfo.imageRef);
  ownerReadback(bundle, env, bundleInfo.sourceSha);
  console.log(`install.mjs: OPL Cloud is running at ${values.get('OPL_PUBLIC_URL') || 'the configured endpoint'}`);
  console.log(`install.mjs: image ${bundleInfo.imageRef}`);
}
if (process.argv[1]) {
  main().catch(error => { console.error(`install.mjs: ${error.message}`); process.exitCode = error.exitCode || 1; });
}

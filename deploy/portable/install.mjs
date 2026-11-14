#!/usr/bin/env node
/**
 * Install or update one OPL Cloud bundle. The bundle is immutable input;
 * installation state lives outside it and keeps configuration, credentials,
 * certificates and data across updates.
 */
import { createHash, randomBytes } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { lookup } from 'node:dns/promises';
import { mkdir, readFile, writeFile, chmod, rename, access, copyFile } from 'node:fs/promises';
import { connect } from 'node:net';
import { dirname, isAbsolute, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readInstallationBundle, parseStrictJson, sha256 } from './installer-manifest.mjs';

const MIN_SECRET = 32;
const PACKAGE_SCHEMA_DIGEST = 'sha256:0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883';
const PUBLISHER_SCHEMA_DIGEST = 'sha256:176d492a0c0c491ca4e180d9e5d3e04a08ab9c621c4254806658ef529c77b5ef';
const COMPOSE_INSTALL_URL = 'https://docs.docker.com/compose/install/';
const DOCKER_INSTALL_URL = 'https://docs.docker.com/engine/install/';
const PRECONDITION_TIMEOUT_MS = 5000;
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
function compose(bundle, env, args) {
  return execFileSync('docker', ['compose', '--env-file', '/dev/null', '-p', 'opl-cloud', ...args], {
    cwd: bundle, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe']
  });
}
function oneLine(text) { return String(text ?? '').replace(/\s+/g, ' ').trim().slice(0, 240); }
function versionParts(text) {
  const match = /(\d+)\.(\d+)(?:\.(\d+))?/.exec(String(text ?? ''));
  return match ? [Number(match[1]), Number(match[2]), Number(match[3] ?? 0)] : null;
}
// The Compose version is a host fact, so it is read before the first Compose
// call. There is no version floor: the published compose file is the only
// requirement, and the parse below decides whether this host can read it. The
// version is still read, because the refusal names the version the host has
// and the operator needs to know what to upgrade.
function readComposeVersion(env) {
  let reported = '';
  try {
    reported = execFileSync('docker', ['compose', 'version'], { env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
  } catch (error) {
    if (error.code === 'ENOENT') {
      fail(`docker_command_missing\nThe installer needs the Docker CLI on PATH. Install Docker Engine (${DOCKER_INSTALL_URL}), then run this command again.`, 2);
    }
    const stderr = oneLine(error.stderr);
    if (/is not a docker command|unknown docker command|unknown command/i.test(stderr)) {
      fail(`docker_compose_plugin_missing\nThe Docker CLI on PATH has no "compose" subcommand. This installer drives the Docker Compose v2 plugin (docker compose); the legacy standalone docker-compose binary is not used. Install the plugin (${COMPOSE_INSTALL_URL}), then run this command again.`, 2);
    }
    fail(`docker_compose_version_unreadable\n"docker compose version" failed: ${stderr || oneLine(error.message)}\nEnsure the Docker Compose plugin is installed and working (${COMPOSE_INSTALL_URL}), then run this command again.`, 2);
  }
  const parts = versionParts(reported);
  if (!parts) {
    fail(`docker_compose_version_unreadable\n"docker compose version" did not report a version: ${oneLine(reported) || '(no output)'}\nEnsure the Docker Compose plugin is installed and working (${COMPOSE_INSTALL_URL}), then run this command again.`, 2);
  }
  return parts.join('.');
}
// The complaint is the Compose parser's own words, and the version is the
// version the host actually has. A merge tag this Compose does not know is the
// usual reason, and it is the reason an upgrade fixes.
function composeParseFailure(version, complaint) {
  const mergeTag = /!override|!reset|cannot override/i.test(complaint)
    ? 'The published compose file uses the !override or !reset merge tags, and this Docker Compose does not apply them.\n'
    : '';
  return `docker_compose_parse_failed\nThe installed Docker Compose (${version}) cannot read the published compose file compose.cloud.yaml.\nThe Compose parser reported: ${complaint}\n${mergeTag}Upgrade Docker Compose (${COMPOSE_INSTALL_URL}), then run this command again.`;
}
// The published compose file is the installation surface, so the only Compose
// requirement is that the installed Compose can read that file. The parse runs
// on the resolved environment, before the installation generates its PKI,
// records its checked version or pulls anything, so a refusal leaves the
// installation as it was.
function requireComposeParses(bundle, env, version) {
  let raw;
  try {
    raw = compose(bundle, env, ['-f', 'compose.cloud.yaml', 'config', '--format', 'json']);
  } catch (error) {
    fail(composeParseFailure(version, oneLine(error.stderr) || oneLine(error.message)), 2);
  }
  try { return JSON.parse(raw); }
  catch { fail(composeParseFailure(version, 'the Compose parser returned no readable configuration'), 2); }
}
function resolveWithTimeout(host, timeoutMs) {
  let timer;
  const expiry = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error('resolution timed out')), timeoutMs);
    if (typeof timer.unref === 'function') timer.unref();
  });
  return Promise.race([lookup(host), expiry]).finally(() => clearTimeout(timer));
}
function connectWithTimeout(address, port, timeoutMs) {
  return new Promise((resolve, reject) => {
    let timer;
    const socket = connect({ host: address, port });
    const finish = (error) => {
      clearTimeout(timer);
      socket.removeAllListeners();
      socket.destroy();
      if (error) reject(error); else resolve();
    };
    timer = setTimeout(() => finish(new Error('connection timed out')), timeoutMs);
    socket.once('connect', () => finish(null));
    socket.once('error', error => finish(error));
  });
}
// The Sub2API authority is an external service, so the installer proves it is
// reachable before it pulls anything. The probe resolves the host and opens a
// TCP connection. It sends no credential, asks no question and verifies no
// identity: only the host part of the URL is ever read, printed or stored.
async function requireSub2apiAuthority(baseUrl) {
  const name = 'OPL_SUB2API_BASE_URL';
  const shape = `Set ${name} to the installation's absolute https URL for the Sub2API authority, for example https://sub2api.example.com. Its host must resolve from this machine and accept TCP connections.`;
  let url;
  try { url = new URL(String(baseUrl)); }
  catch { fail(`sub2api_authority_url_invalid:${name}\n${name} is not an absolute URL the installer can read.\n${shape}`, 2); }
  if (!['http:', 'https:'].includes(url.protocol)) {
    fail(`sub2api_authority_url_invalid:${name}\n${name} must use the http or https scheme.\n${shape}`, 2);
  }
  const host = url.hostname.replace(/^\[/, '').replace(/\]$/, '');
  if (!host) fail(`sub2api_authority_url_invalid:${name}\n${name} names no host.\n${shape}`, 2);
  const port = Number(url.port || (url.protocol === 'https:' ? 443 : 80));
  let address;
  try { address = (await resolveWithTimeout(host, PRECONDITION_TIMEOUT_MS)).address; }
  catch { fail(`sub2api_authority_unresolvable:${name}\nThe host ${host} named by ${name} did not resolve from this machine.\n${shape}`, 2); }
  try { await connectWithTimeout(address, port, PRECONDITION_TIMEOUT_MS); }
  catch { fail(`sub2api_authority_unreachable:${name}\nThe host ${host} named by ${name} did not accept a TCP connection on port ${port}.\n${shape}`, 2); }
  return { host, port };
}
// The installer's own fact discovery reads the published compose file. The
// "--variables" command of "docker compose config" shipped in Compose v2.26.0,
// so a Docker Compose that does not know that flag cannot report the
// variables the file declares. Such a Compose reads the same file itself, and
// the only Compose requirement stays the one the file states: the published
// compose file must be parseable.
async function requiredVariables(bundle, version) {
  const env = cleanEnvironment();
  let raw = null;
  try {
    raw = compose(bundle, env, ['-f', 'compose.cloud.yaml', 'config', '--variables', '--format', 'json']);
  } catch (error) {
    const complaint = oneLine(error.stderr) || oneLine(error.message);
    if (!/unknown flag|unknown option/i.test(complaint)) fail(composeParseFailure(version, complaint), 2);
  }
  let model = null;
  if (raw !== null) { try { model = JSON.parse(raw); } catch { fail('compose_variables_invalid'); } }
  const required = new Set();
  if (model) {
    for (const item of Object.values(model).filter(item => item.Required || (item.DefaultValue === '' && item.PresenceValue === ''))) required.add(item.Name);
  }
  for (const file of ['compose.cloud.yaml']) {
    let text;
    try { text = await readFile(join(bundle, file), 'utf8'); } catch { continue; }
    for (const match of text.matchAll(/(?<!\$)\$\{([A-Za-z_][A-Za-z0-9_]*)([^}]*)\}/g)) {
      const modifier = match[2];
      // A Compose variable is required when the file asks for it and no
      // default stands in for it. "$${...}" is an escaped shell variable,
      // not a Compose variable, and never a fact.
      if (modifier === '' || modifier.startsWith(':?') || modifier.startsWith('?') || modifier === ':-' || modifier === '-') required.add(match[1]);
    }
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
  // Two criteria, and each identity is probed by the one that belongs to it.
  // An owner is a data owner and is probed over its gRPC health service. The
  // Control Plane and the Console BFF are deployment units rather than data
  // owners: they carry a transport identity, not an owner identity, so they
  // are probed over the HTTP surface each one serves.
  const probes = [
    ['tenant', 'tenant', '127.0.0.1:8187'], ['resource-catalog', 'resource_catalog', '127.0.0.1:8186'],
    ['ledger', 'ledger', '127.0.0.1:8189'], ['capability', 'capability', '127.0.0.1:8181'],
    ['build', 'build', '127.0.0.1:8182'], ['runtime-control', 'runtime_control', '127.0.0.1:8183'],
    ['workspace', 'workspace', '127.0.0.1:8184'], ['serve', 'serve', '127.0.0.1:8185'],
    ['fabric', 'fabric', '127.0.0.1:8188'], ['console-bff', 'console_bff', 'http://127.0.0.1:8190'],
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
  const composeVersion = readComposeVersion(cleanEnvironment());
  const statePath = join(options.installDir, 'installation.json');
  const state = await readState(statePath);
  if (state && state.schemaVersion !== 1) fail('installation_state_version_unsupported');
  if (state && state.imageRef === bundleInfo.imageRef && state.sourceSha !== bundleInfo.sourceSha) fail('installation_identity_conflict');
  await mkdir(options.installDir, { recursive: true, mode: 0o700 });
  const defaultPkiDir = join(options.installDir, 'pki');
  const dataDir = join(options.installDir, 'data');
  await mkdir(dataDir, { recursive: true, mode: 0o700 });
  const template = parseTemplate(await readFile(join(bundle, 'opl-cloud.env.example'), 'utf8'));
  const required = await requiredVariables(bundle, composeVersion);
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
  // The published compose file is parsed here, on the resolved environment and
  // before the installation generates its PKI, records the version it was
  // checked against or pulls anything. The parse is the precondition: it
  // refuses exactly the Docker Compose versions that cannot read the file this
  // bundle ships.
  const config = requireComposeParses(bundle, env, composeVersion);
  console.log(`install.mjs: Docker Compose ${composeVersion} parsed the published compose file`);
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
  next.composeVersion = composeVersion;
  await atomicJson(statePath, next);
  if (options.dryRun) { console.log(`install.mjs: dry run verified ${bundleInfo.imageRef}`); return; }
  const authority = await requireSub2apiAuthority(values.get('OPL_SUB2API_BASE_URL'));
  console.log(`install.mjs: Sub2API authority ${authority.host}:${authority.port} is reachable (network reachability only; no credential was sent or verified)`);
  compose(bundle, env, ['-f', 'compose.cloud.yaml', 'pull']);
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

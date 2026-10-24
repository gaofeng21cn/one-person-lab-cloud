// The local v2 installation facts.
//
// deploy/portable/opl-cloud.env.example is the only declaration of an
// installation's variables, and deploy/portable/compose.local-v2.yaml is the only
// declaration of which of those variables the local v2 topology reads. This
// module derives one installation's environment file from those two files, so
// the topology's inputs are never hand-written and never drift from the
// template that declares them.
//
// The derivation is a check as much as a fill. It refuses to produce an
// environment file when
//
//   - the topology reads a variable the template does not declare, because the
//     installation would run on a value nobody declared;
//   - a variable the topology reads still holds a placeholder, because a
//     placeholder is an unfilled fact and a raw "<" is a redirection operator to
//     any shell that reads the file;
//   - the template declares one name twice, because the Compose environment
//     reader keeps the last definition and the two readers of the same file
//     would then disagree;
//   - two database roles share one password, or one password is shorter than
//     the PostgreSQL initialization refuses.
//
// What it does not do is invent a fact. A placeholder the topology reads has to
// be supplied by the caller as a fact or a secret; this module never guesses
// one, because a guessed address or a guessed digest is an installation that
// looks real and is not.

import { randomBytes } from "node:crypto";

// The transport contract refuses a shorter identity token
// (packages/contracts/go/owneridentity.MinimumTokenLength).
export const LOCAL_V2_MINIMUM_TOKEN_LENGTH = 32;

// The PostgreSQL initialization refuses a shorter password and refuses two
// roles that share one.
export const LOCAL_V2_MINIMUM_PASSWORD_LENGTH = 32;

const ASSIGNMENT = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/;
// Every placeholder in the template is an angle-bracketed token, whether it
// names a secret ("<32-plus-random-chars>") or a fact
// ("<replace-with-tencent-zone>").
const PLACEHOLDER = /<[^>\n]+>/g;

// The token a caller presents to one target owner. The target's allowlist
// carries that one value for every caller it admits, which is the single
// spelling the owner topology declares ("an installation must set both
// spellings for the same target").
//
// The tenant deployment unit carries the CloudIdentity and the Gateway owners
// in one process, so the value every caller presents to it is the CloudIdentity
// token rather than a tenant token.
export function localV2TargetTokenVariable(peerTokensEnv) {
  const name = String(peerTokensEnv || "");
  if (!name.startsWith("OPL_") || !name.endsWith("_PEER_TOKENS")) {
    throw localV2FactsError("local_v2_peer_tokens_name_invalid");
  }
  const target = name.slice("OPL_".length, -"_PEER_TOKENS".length).toLowerCase();
  if (target === "tenant") return "OPL_CLOUD_IDENTITY_TOKEN";
  return `OPL_${target.toUpperCase()}_TOKEN`;
}

export function localV2FactsError(code, detail = "") {
  const error = new Error(detail ? `${code}: ${detail}` : code);
  error.code = code;
  return error;
}

// Every variable the compose file reads.
//
// The scan follows the Compose reader's own escaping: a "$$" pair is a literal
// "$", so a shell expression inside an inline configuration ("eval
// "password=\$${OPL_$${upper}_DATABASE_PASSWORD}"") is not a variable
// reference and is not read as one. A reference is a "${" that is not escaped,
// followed by a name and a terminator.
export function localV2ReferencedVariables(compose) {
  const text = String(compose || "");
  const names = new Set();
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] !== "$") continue;
    if (text[index + 1] === "$") {
      index += 1;
      continue;
    }
    if (text[index + 1] !== "{") continue;
    const rest = text.slice(index + 2);
    const match = /^([A-Za-z_][A-Za-z0-9_]*)([:?\-}]|$)/.exec(rest);
    if (!match) continue;
    names.add(match[1]);
    index += 1;
  }
  return [...names].sort();
}

// The declarations of one template, in the order the template declares them.
//
// A name the template declares twice is a defect of the template: the Compose
// environment reader keeps the last definition and the first reader of the same
// file keeps the first one, so the two spellings of one installation fact can
// disagree. The declarations are still returned, with the last value, because
// that is the value the Compose reader takes; the duplication is reported
// rather than hidden, and the caller refuses it when the topology reads that
// name.
export function localV2TemplateDeclarations(template) {
  const declarations = [];
  const index = new Map();
  const duplicates = [];
  for (const raw of String(template || "").split("\n")) {
    const match = ASSIGNMENT.exec(raw);
    if (!match) continue;
    const name = match[1];
    if (index.has(name)) {
      const previous = declarations[index.get(name)];
      duplicates.push({ name, values: [previous.value, match[2]] });
      previous.value = match[2];
      continue;
    }
    index.set(name, declarations.length);
    declarations.push({ name, value: match[2] });
  }
  return { declarations, duplicates };
}

// A fact this installation supplies that is not a secret: an address, a
// digest, a path, a profile.
export function localV2RandomSecret(bytes = 32) {
  return randomBytes(bytes).toString("hex");
}

/*
 * Derive one installation's environment entries.
 *
 * `values` carries everything this installation supplies, facts and secrets
 * alike; a caller keeps them apart when it matters (a secret must not be
 * logged) and this module does not need to know which is which. A name the
 * topology reads that the template leaves as a placeholder and the caller did
 * not supply is refused rather than filled.
 */
// A sentinel is the shape an unfilled fact takes: "unset-<name>" for a whole
// value, and the same marker inside a structured value. It is recognized
// anywhere in the value, because a structured value keeps its shape and the
// sentinel is embedded in it.
function localV2IsUnfilled(value) {
  return /(^|[^A-Za-z0-9_-])unset-[a-z0-9_]+/.test(String(value || ""));
}

export function localV2InstallationFacts({ template, compose, values = {} } = {}) {
  const { declarations, duplicates } = localV2TemplateDeclarations(template);
  const referenced = localV2ReferencedVariables(compose);
  const declared = new Set(declarations.map((declaration) => declaration.name));

  const missing = referenced.filter((name) => !declared.has(name));
  if (missing.length) {
    throw localV2FactsError("local_v2_variable_not_declared", missing.join(", "));
  }

  // A name the topology reads and the template declares twice has two
  // spellings, and the topology would run on one of them without saying which.
  const conflicting = duplicates
    .filter((duplicate) => referenced.includes(duplicate.name))
    .map((duplicate) => duplicate.name);
  if (conflicting.length) {
    throw localV2FactsError("local_v2_variable_declared_twice", conflicting.join(", "));
  }

  const entries = [];
  const resolved = new Map();
  const unresolved = [];

  for (const { name, value } of declarations) {
    if (name.endsWith("_PEER_TOKENS")) {
      const tokenVariable = localV2TargetTokenVariable(name);
      const token = values[tokenVariable];
      if (typeof token !== "string" || token.trim().length < LOCAL_V2_MINIMUM_TOKEN_LENGTH) {
        throw localV2FactsError("local_v2_target_token_missing", `${name} needs ${tokenVariable}`);
      }
      let allowlist;
      try {
        allowlist = JSON.parse(value);
      } catch {
        throw localV2FactsError("local_v2_peer_tokens_not_json", name);
      }
      if (!allowlist || typeof allowlist !== "object" || Array.isArray(allowlist)) {
        throw localV2FactsError("local_v2_peer_tokens_not_json", name);
      }
      for (const peer of Object.keys(allowlist)) {
        if (!String(allowlist[peer]).startsWith("<")) {
          throw localV2FactsError("local_v2_peer_token_not_a_template", `${name}:${peer}`);
        }
        allowlist[peer] = token;
      }
      const rendered = JSON.stringify(allowlist);
      // The allowlist is derived, never supplied: it is one spelling of the
      // target's token, and a second spelling that disagrees with the token the
      // callers present is the defect the owner topology declares against.
      if (Object.prototype.hasOwnProperty.call(values, name) && String(values[name]) !== rendered) {
        throw localV2FactsError("local_v2_peer_tokens_conflict", name);
      }
      resolved.set(name, rendered);
      entries.push([name, rendered]);
      continue;
    }
    if (Object.prototype.hasOwnProperty.call(values, name)) {
      const supplied = String(values[name]);
      resolved.set(name, supplied);
      entries.push([name, supplied]);
      continue;
    }
    if (value.startsWith("<") && value.endsWith(">")) {
      // A placeholder that belongs to another deployment mode. It is written as
      // a shell-safe sentinel, and the check below refuses to run the topology
      // when the topology reads one.
      resolved.set(name, `unset-${name.toLowerCase()}`);
      entries.push([name, `unset-${name.toLowerCase()}`]);
      unresolved.push(name);
      continue;
    }
    if (PLACEHOLDER.test(value)) {
      // A placeholder embedded in a structured value, such as the provider
      // profile of a deployment mode this installation does not select. It
      // keeps its shape and loses its raw placeholder.
      PLACEHOLDER.lastIndex = 0;
      const filled = value.replace(PLACEHOLDER, (token) => `unset-${token.slice(1, -1).toLowerCase()}`);
      resolved.set(name, filled);
      entries.push([name, filled]);
      unresolved.push(name);
      continue;
    }
    resolved.set(name, value);
    entries.push([name, value]);
  }

  // A sentinel the topology reads is not a value.
  const unfilled = referenced.filter((name) => localV2IsUnfilled(resolved.get(name)));
  if (unfilled.length) {
    throw localV2FactsError("local_v2_variable_unfilled", unfilled.join(", "));
  }

  // A raw placeholder anywhere in the output is a defect: it is an unfilled
  // fact, and it is a redirection operator to any shell that reads the file.
  const residual = entries.filter(([, value]) => value.includes("<") && value.includes(">"));
  if (residual.length) {
    throw localV2FactsError("local_v2_raw_placeholder", residual.map(([name]) => name).join(", "));
  }

  const passwords = entries
    .filter(([name]) => name.endsWith("_DATABASE_PASSWORD") || name === "OPL_POSTGRES_ADMIN_PASSWORD")
    .map(([name, value]) => [name, value]);
  const short = passwords.filter(([, value]) => value.length < LOCAL_V2_MINIMUM_PASSWORD_LENGTH);
  if (short.length) {
    throw localV2FactsError("local_v2_password_too_short", short.map(([name]) => name).join(", "));
  }
  if (new Set(passwords.map(([, value]) => value)).size !== passwords.length) {
    throw localV2FactsError("local_v2_password_shared", passwords.map(([name]) => name).join(", "));
  }

  return { entries, values: Object.fromEntries(resolved), referenced, unresolved, duplicates };
}

// The environment file the Compose environment reader consumes. It declares
// every name exactly once, in the template's order, so the file and the
// template read the same way.
export function localV2EnvFile(facts, { header = "" } = {}) {
  const lines = [];
  if (header) lines.push(`# ${header}`);
  for (const [name, value] of facts.entries) lines.push(`${name}=${value}`);
  return `${lines.join("\n")}\n`;
}

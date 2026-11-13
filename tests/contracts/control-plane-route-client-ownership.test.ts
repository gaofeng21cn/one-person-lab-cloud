import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import test from "node:test";

// Route ownership is a boundary, not a convention.
//
// Every HTTP route a Cloud process registers has to be able to name the client
// that calls it: the Console browser, an operator tool, or the installation's
// own probe. A route whose only client is the test that exercises it is a
// route nobody can reach in production, and it is the shape that lets an old
// surface outlive the product that used it.
//
// This test derives both sides from the owning source. The route set comes
// from the Control Plane's own net/http registrations, read with Go's parser
// so a comment or a response body cannot claim a route. The client set comes
// from the Console UI and the operator tools, read by resolving the path
// argument each request call actually passes - including the paths that only
// exist as a template literal, a conditional between the retained and the
// cloud identity, or a helper that composes a base path with a suffix.
//
// Nothing here is a hand written route list. Delete a route and this test
// still knows the full set; add a client and it still knows.

const CONTROL_PLANE_SERVER_DIR = "services/control-plane/internal/server";
// Where each browser facing process keeps its route registrations, so the
// contract's declaration can be checked against the owning source.
const BROWSER_FACE_PROCESS_DIRECTORIES: Record<string, string> = {
  "control-plane": CONTROL_PLANE_SERVER_DIR,
  "console-bff": "apps/console-bff/internal/httpapi",
};
const ROUTE_SCAN = `${CONTROL_PLANE_SERVER_DIR}/testdata/route-scan/main.go`;
const TOPOLOGY_PATH = "deploy/portable/opl-cloud-owner-topology.json";

// A path segment whose value the source does not spell out. It still matches
// a route segment, because the caller may pass any value there.
const WILDCARD = "\uE000";

type ScannedRoute = { method: string; path: string; file: string; line: number };
type RouteInventory = {
  routes: ScannedRoute[];
  unresolved: { expression: string; file: string; line: number }[];
};

type ClientCall = {
  file: string;
  line: number;
  method: string;
  path: string;
  callee: string;
};

// ---------------------------------------------------------------- route side

function goFiles(directory: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) found.push(...goFiles(full));
    else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) found.push(full);
  }
  return found;
}

function scanRoutes(directory: string): RouteInventory {
  const files = goFiles(directory);
  const output = execFileSync("go", ["run", ROUTE_SCAN], {
    input: JSON.stringify(files),
    encoding: "utf8",
    maxBuffer: 4 * 1024 * 1024,
  });
  return JSON.parse(output) as RouteInventory;
}

function controlPlaneRoutes(): RouteInventory {
  return scanRoutes(CONTROL_PLANE_SERVER_DIR);
}

// -------------------------------------------------------------- client side

type FunctionInfo = { params: string[]; body: string; returns: string[]; bodyStart: number; bodyEnd: number };
type Scope = {
  consts: Map<string, string[]>;
  functions: Map<string, FunctionInfo>;
  bindings: Map<string, string[]>;
  // The parameters of the function a call site sits in. A name that names a
  // parameter there is that parameter's value, never a constant of the same
  // name somewhere else in the file.
  shadowed: Set<string>;
};

// maskSource blanks out every comment, string and template literal while
// keeping the offsets and the line breaks of the original source, so the
// structural scan below never trips over a path inside a comment.
function maskSource(source: string): string {
  const masked = source.split("");
  const blank = (from: number, to: number): void => {
    for (let index = from; index < to && index < masked.length; index += 1) {
      if (masked[index] !== "\n") masked[index] = " ";
    }
  };
  let index = 0;
  while (index < source.length) {
    const char = source[index];
    if (char === "/" && source[index + 1] === "/") {
      const end = source.indexOf("\n", index);
      blank(index, end < 0 ? source.length : end);
      index = end < 0 ? source.length : end;
      continue;
    }
    if (char === "/" && source[index + 1] === "*") {
      const end = source.indexOf("*/", index + 2);
      const stop = end < 0 ? source.length : end + 2;
      blank(index, stop);
      index = stop;
      continue;
    }
    if (char === '"' || char === "'" || char === "`") {
      const end = literalEnd(source, index);
      blank(index, end);
      index = end;
      continue;
    }
    index += 1;
  }
  return masked.join("");
}

// literalEnd returns the offset just past the string or template literal
// that starts at `start`.
function literalEnd(source: string, start: number): number {
  const quote = source[start];
  let index = start + 1;
  while (index < source.length) {
    if (source[index] === "\\" && quote !== "`") {
      index += 2;
      continue;
    }
    if (source[index] === quote) return index + 1;
    index += 1;
  }
  return source.length;
}

// matchingBracket returns the offset of the bracket that closes the one at
// `start`, skipping strings, templates and comments.
function matchingBracket(source: string, mask: string, start: number, open: string, close: string): number {
  let depth = 0;
  let index = start;
  while (index < source.length) {
    const char = mask[index];
    if (char === open) depth += 1;
    else if (char === close) {
      depth -= 1;
      if (depth === 0) return index;
    }
    index += 1;
  }
  return -1;
}

// topLevelSplit splits `text` on `separator` characters that are not nested
// inside brackets, strings or templates.
function topLevelSplit(text: string, mask: string, separator: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let current = "";
  let index = 0;
  while (index < text.length) {
    const char = mask[index];
    if (char === "(" || char === "[" || char === "{" || char === "<") depth += 1;
    else if (char === ")" || char === "]" || char === "}" || char === ">") depth -= 1;
    if (depth === 0 && char === separator) {
      parts.push(current);
      current = "";
      index += 1;
      continue;
    }
    current += text[index];
    index += 1;
  }
  parts.push(current);
  return parts;
}

function collectScope(source: string, mask: string): { consts: Map<string, string[]>; functions: Map<string, FunctionInfo> } {
  const consts = new Map<string, string[]>();
  const functions = new Map<string, FunctionInfo>();

  const constPattern = /(?:^|[^\w$.])(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=/g;
  let match: RegExpExecArray | null;
  while ((match = constPattern.exec(mask)) !== null) {
    const name = match[1];
    const equals = mask.indexOf("=", match.index + match[0].length - 1);
    const end = statementEnd(mask, equals + 1);
    const initializer = source.slice(equals + 1, end).trim();
    const existing = consts.get(name);
    if (existing) existing.push(initializer);
    else consts.set(name, [initializer]);
  }

  const functionPattern = /(?:^|[^\w$.])(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)/g;
  while ((match = functionPattern.exec(mask)) !== null) {
    const name = match[1];
    const open = mask.indexOf("(", match.index + match[0].length - 1);
    if (open < 0) continue;
    const close = matchingBracket(source, mask, open, "(", ")");
    if (close < 0) continue;
    const params = parameterNames(source.slice(open + 1, close));
    const bodyOpen = mask.indexOf("{", close);
    if (bodyOpen < 0) continue;
    const bodyClose = matchingBracket(source, mask, bodyOpen, "{", "}");
    if (bodyClose < 0) continue;
    const body = source.slice(bodyOpen, bodyClose + 1);
    functions.set(name, { params, body, returns: returnExpressions(body), bodyStart: bodyOpen, bodyEnd: bodyClose });
  }
  return { consts, functions };
}

// statementEnd returns the offset of the `;` that closes a statement.
function statementEnd(mask: string, from: number): number {
  let depth = 0;
  let index = from;
  while (index < mask.length) {
    const char = mask[index];
    if (char === "(" || char === "[" || char === "{") depth += 1;
    else if (char === ")" || char === "]" || char === "}") depth -= 1;
    else if (char === ";" && depth === 0) return index;
    index += 1;
  }
  return mask.length;
}

function parameterNames(text: string): string[] {
  const names: string[] = [];
  for (const part of text.split(",")) {
    const trimmed = part.trim().replace(/=.*$/, "").trim();
    const name = trimmed.split(":")[0].trim();
    if (/^[A-Za-z_$][\w$]*$/.test(name)) names.push(name);
    else names.push("");
  }
  return names;
}

// returnExpressions reads the expressions a function returns.
function returnExpressions(body: string): string[] {
  const mask = maskSource(body);
  const returns: string[] = [];
  const pattern = /(?:^|[^\w$.])return\s/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(mask)) !== null) {
    const start = match.index + match[0].length;
    const end = statementEnd(mask, start);
    const expression = body.slice(start, end).trim();
    if (expression) returns.push(expression);
  }
  return returns;
}

// resolveExpression turns a source expression into the concrete path strings
// it can produce. It returns null when the source does not spell the value
// out, which the caller treats as an unknown segment rather than a guess.
function resolveExpression(text: string, scope: Scope, depth = 0): string[] | null {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  // A helper that composes a path can call another helper. The chain is short
  // in practice; a deeper one means the source says something this reader
  // cannot follow, and guessing would be worse than stopping.
  if (depth > 16) return null;

  if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
    return [unquote(trimmed)];
  }
  if (trimmed.startsWith("`")) return resolveTemplate(trimmed, scope, depth + 1);

  const call = splitCall(trimmed);
  if (call) {
    const definition = scope.functions.get(call.name);
    if (!definition) return null;
    const bindings = new Map(scope.bindings);
    definition.params.forEach((param, index) => {
      const argument = call.args[index];
      if (param && argument !== undefined) bindings.set(param, [argument]);
    });
    const inner: Scope = { ...scope, bindings, shadowed: new Set() };
    const resolved: string[] = [];
    for (const expression of definition.returns) {
      const value = resolveExpression(expression, inner, depth + 1);
      if (!value) return null;
      resolved.push(...value);
    }
    return resolved.length > 0 ? resolved : null;
  }

  const member = /^([A-Za-z_$][\w$]*)\.([A-Za-z_$][\w$]*)$/.exec(trimmed);
  if (member) return resolveMember(member[1], member[2], scope);

  const ternary = splitTernary(trimmed);
  if (ternary) {
    const whenTrue = resolveExpression(ternary.whenTrue, scope, depth + 1);
    const whenFalse = resolveExpression(ternary.whenFalse, scope, depth + 1);
    if (!whenTrue || !whenFalse) return null;
    return [...whenTrue, ...whenFalse];
  }


  if (/^[A-Za-z_$][\w$]*$/.test(trimmed)) {
    const bound = scope.bindings.get(trimmed);
    if (bound) {
      const resolved: string[] = [];
      for (const expression of bound) {
        const value = resolveExpression(expression, scope, depth + 1);
        if (!value) return null;
        resolved.push(...value);
      }
      return resolved;
    }
    if (scope.shadowed.has(trimmed)) return null;
    const initializers = scope.consts.get(trimmed);
    if (initializers) {
      const resolved: string[] = [];
      for (const expression of initializers) {
        const value = resolveExpression(expression, scope, depth + 1);
        if (!value) return null;
        resolved.push(...value);
      }
      return resolved;
    }
    return null;
  }

  const plus = splitPlus(trimmed);
  if (plus) {
    const left = resolveExpression(plus.left, scope, depth + 1);
    const right = resolveExpression(plus.right, scope, depth + 1);
    if (!left || !right) return null;
    const combined: string[] = [];
    for (const a of left) for (const b of right) combined.push(a + b);
    return combined;
  }
  return null;
}

// resolveMember reads `base.property` out of an object literal.
function resolveMember(base: string, property: string, scope: Scope): string[] | null {
  const initializers = scope.consts.get(base);
  if (!initializers) return null;
  const resolved: string[] = [];
  for (const initializer of initializers) {
    const value = objectProperty(initializer, property);
    if (value === null) return null;
    const result = resolveExpression(value, scope, depth + 1);
    if (!result) return null;
    resolved.push(...result);
  }
  return resolved;
}

// objectProperty reads one property out of an object literal.
function objectProperty(initializer: string, property: string): string | null {
  const trimmed = initializer.trim();
  if (!trimmed.startsWith("{")) return null;
  const mask = maskSource(trimmed);
  const pattern = new RegExp(`(?:^|[^\\w$.])${property}\\s*:`, "g");
  const match = pattern.exec(mask);
  if (!match) return null;
  const start = match.index + match[0].length;
  const end = statementEnd(mask, start);
  const value = trimmed.slice(start, end).trim();
  return value === "" ? null : value;
}

function resolveTemplate(text: string, scope: Scope, depth = 0): string[] | null {
  const parts = templateParts(text);
  let combinations: string[][] = [[]];
  for (const part of parts) {
    if (part.literal !== undefined) {
      combinations = combinations.map((combination) => [...combination, part.literal]);
      continue;
    }
    const resolved = resolveExpression(part.expression ?? "", scope, depth + 1);
    if (!resolved) {
      combinations = combinations.map((combination) => [...combination, WILDCARD]);
      continue;
    }
    const next: string[][] = [];
    for (const combination of combinations) for (const value of resolved) next.push([...combination, value]);
    combinations = next;
  }
  return combinations.map((combination) => combination.join(""));
}

type TemplatePart = { literal?: string; expression?: string };

function templateParts(text: string): TemplatePart[] {
  const parts: TemplatePart[] = [];
  let literal = "";
  let index = 1;
  while (index < text.length) {
    const char = text[index];
    if (char === "\\") {
      literal += text[index + 1];
      index += 2;
      continue;
    }
    if (char === "`") break;
    if (char === "$" && text[index + 1] === "{") {
      parts.push({ literal });
      literal = "";
      const expression = readBracedExpression(text, index + 2);
      parts.push({ expression: expression.text });
      index = expression.end;
      continue;
    }
    literal += char;
    index += 1;
  }
  parts.push({ literal });
  return parts;
}

// readBracedExpression reads the expression inside `${...}`.
function readBracedExpression(text: string, start: number): { text: string; end: number } {
  const mask = maskSource(text.slice(start));
  let depth = 0;
  for (let index = 0; index < mask.length; index += 1) {
    const char = mask[index];
    if (char === "{" || char === "(" || char === "[") depth += 1;
    else if (char === "}" || char === ")" || char === "]") {
      if (depth === 0 && char === "}") return { text: text.slice(start, start + index), end: start + index + 1 };
      depth -= 1;
    }
  }
  return { text: text.slice(start), end: text.length };
}

function splitCall(text: string): { name: string; args: string[] } | null {
  const match = /^([A-Za-z_$][\w$]*)\s*\(/.exec(text);
  if (!match) return null;
  const name = match[1];
  const open = text.indexOf("(", match.index);
  const close = matchingBracket(text, maskSource(text), open, "(", ")");
  if (close < 0) return null;
  const arguments_ = text.slice(open + 1, close);
  return { name, args: topLevelSplit(arguments_, maskSource(arguments_), ",").map((part) => part.trim()) };
}

function splitTernary(text: string): { whenTrue: string; whenFalse: string } | null {
  const mask = maskSource(text);
  const question = topLevelIndex(mask, "?");
  if (question < 0) return null;
  const colon = topLevelIndex(mask.slice(question + 1), ":");
  if (colon < 0) return null;
  return {
    whenTrue: text.slice(question + 1, question + 1 + colon).trim(),
    whenFalse: text.slice(question + 1 + colon + 1).trim(),
  };
}

function splitPlus(text: string): { left: string; right: string } | null {
  const mask = maskSource(text);
  const plus = topLevelIndex(mask, "+");
  if (plus < 0) return null;
  return { left: text.slice(0, plus).trim(), right: text.slice(plus + 1).trim() };
}

function topLevelIndex(mask: string, character: string): number {
  let depth = 0;
  for (let index = 0; index < mask.length; index += 1) {
    const char = mask[index];
    if (char === "(" || char === "[" || char === "{") depth += 1;
    else if (char === ")" || char === "]" || char === "}") depth -= 1;
    else if (depth === 0 && char === character) return index;
  }
  return -1;
}

function unquote(text: string): string {
  const body = text.slice(1, -1);
  return body.replace(/\\(["'`\\nrt])/g, (_match, escaped: string) => {
    if (escaped === "n") return "\n";
    if (escaped === "r") return "\r";
    if (escaped === "t") return "\t";
    return escaped;
  });
}

// ----------------------------------------------------------- request shape

// requestMethods derives, for every function in the file, the HTTP method it
// sends and the index of the parameter that carries the path. A function that
// never performs a request is not a transport and never claims a route.
function requestMethods(functions: Map<string, FunctionInfo>): Map<string, { method: string; pathIndex: number }> {
  const methods = new Map<string, { method: string; pathIndex: number }>();
  const visiting = new Set<string>();

  const derive = (name: string): { method: string; pathIndex: number } | null => {
    const cached = methods.get(name);
    if (cached) return cached;
    if (visiting.has(name)) return null;
    const definition = functions.get(name);
    if (!definition) return null;
    visiting.add(name);

    const pathIndex = Math.max(
      0,
      definition.params.findIndex((param) => param === "path" || param === "url" || param === "endpoint"),
    );
    let method: string | null = null;

    const writeJson = /writeJson[^()]*\(\s*"([A-Z]+)"/.exec(definition.body);
    if (writeJson) method = writeJson[1];
    if (!method) {
      const literalMethod = /method\s*:\s*"([A-Z]+)"/.exec(definition.body);
      if (literalMethod) method = literalMethod[1];
    }
    if (!method) {
      for (const callee of functions.keys()) {
        if (callee === name) continue;
        if (!definition.body.includes(callee)) continue;
        if (!new RegExp(`[^\\w$.]${callee}\\s*[(<]`).test(definition.body)) continue;
        const derived = derive(callee);
        if (derived) {
          method = derived.method;
          break;
        }
      }
    }
    if (!method && /\bfetch\s*\(/.test(definition.body)) method = "GET";

    visiting.delete(name);
    if (!method) return null;
    const resolved = { method, pathIndex };
    methods.set(name, resolved);
    return resolved;
  };

  for (const name of functions.keys()) derive(name);
  return methods;
}

type SourceFile = { file: string; source: string; mask: string };

function sourceFiles(directory: string): SourceFile[] {
  const found: SourceFile[] = [];
  const walk = (current: string): void => {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!/\.(ts|tsx)$/.test(entry.name)) continue;
      if (entry.name.endsWith(".test.ts") || entry.name.endsWith(".test.tsx")) continue;
      const source = readFileSync(full, "utf8");
      found.push({ file: full, source, mask: maskSource(source) });
    }
  };
  walk(directory);
  return found;
}

// The operator tools that answer HTTP for the installation serve a fixture
// Control Plane instead of calling one. A file that owns a listener or
// intercepts browser traffic is the server side of the boundary, not its
// client.
function servesHttp(file: SourceFile): boolean {
  return /from\s+"node:http"/.test(file.source) || /page\s*\.\s*route\s*\(/.test(file.source);
}

// collectClientCalls reads every request call in the file and resolves the
// path it asks for.
function collectClientCalls(
  file: SourceFile,
  scope: Scope,
  localFunctions: Map<string, FunctionInfo>,
  transports: Map<string, { method: string; pathIndex: number }>,
): ClientCall[] {
  const calls: ClientCall[] = [];
  const pattern = /(?:^|[^\w$.])([A-Za-z_$][\w$]*)\s*(?:<[^<>;{}()]*>)?\s*\(/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(file.mask)) !== null) {
    const callee = match[1];
    const open = file.mask.indexOf("(", match.index + match[0].length - 1);
    const close = matchingBracket(file.source, file.mask, open, "(", ")");
    if (close < 0) continue;
    const arguments_ = file.source.slice(open + 1, close);
    const args = topLevelSplit(arguments_, maskSource(arguments_), ",").map((part) => part.trim());
    const transport = transports.get(callee) ?? (callee === "fetch" ? fetchTransport(arguments_) : undefined);
    if (!transport) continue;
    const argument = args[transport.pathIndex];
    if (argument === undefined) continue;
    const scoped: Scope = { ...scope, shadowed: enclosingParameters(localFunctions, match.index) };
    const resolved = resolveExpression(argument, scoped);
    if (!resolved) continue;
    for (const value of resolved) {
      const trimmed = value.split("?")[0];
      if (!isControlPlaneApiPath(trimmed)) continue;
      calls.push({
        file: file.file,
        line: file.source.slice(0, match.index).split("\n").length,
        method: transport.method,
        path: trimmed,
        callee,
      });
    }
  }
  return calls;
}

// sharedFunctions holds every function the client surface declares. A request
// helper and the call sites that use it often live in different files, so the
// transport lookup has to see the whole surface rather than one file.
function sharedFunctions(): Map<string, FunctionInfo> {
  const all = new Map<string, FunctionInfo>();
  for (const file of [...sourceFiles("apps/console-ui/src"), ...sourceFiles("tools")]) {
    for (const [name, info] of collectScope(file.source, file.mask).functions) all.set(name, info);
  }
  return all;
}

// consoleUiClients reads the browser's own request calls.
function consoleUiClients(shared: Map<string, FunctionInfo>, transports: Map<string, { method: string; pathIndex: number }>): ClientCall[] {
  const files = sourceFiles("apps/console-ui/src");
  const calls: ClientCall[] = [];
  for (const file of files) {
    const local = collectScope(file.source, file.mask);
    calls.push(
      ...collectClientCalls(
        file,
        { ...local, functions: shared, bindings: new Map(), shadowed: new Set() },
        local.functions,
        transports,
      ),
    );
  }
  return calls;
}

// toolClients reads the operator tools' request calls. A tool request names
// its path directly and its method in the options it passes along.
function toolClients(shared: Map<string, FunctionInfo>): ClientCall[] {
  const files = sourceFiles("tools");
  const calls: ClientCall[] = [];
  for (const file of files) {
    if (servesHttp(file)) continue;
    const local = collectScope(file.source, file.mask);
    const scope: Scope = { ...local, functions: shared, bindings: new Map(), shadowed: new Set() };
    const localFunctions = local.functions;
    const pattern = /(?:^|[^\w$.])([A-Za-z_$][\w$]*)\s*\.\s*([A-Za-z_$][\w$]*)\s*\(/g;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(file.mask)) !== null) {
      const property = match[2];
      if (!/^(json|get|post|put|patch|delete|request|fetch|send)$/i.test(property)) continue;
      const open = file.mask.indexOf("(", match.index + match[0].length - 1);
      const close = matchingBracket(file.source, file.mask, open, "(", ")");
      if (close < 0) continue;
      const arguments_ = file.source.slice(open + 1, close);
      const args = topLevelSplit(arguments_, maskSource(arguments_), ",").map((part) => part.trim());
      const argument = args[0];
      if (argument === undefined) continue;
      const scoped: Scope = { ...scope, shadowed: enclosingParameters(localFunctions, match.index) };
      const resolved = resolveExpression(argument, scoped);
      if (!resolved) continue;
      const method = /method\s*:\s*"([A-Z]+)"/.exec(file.source.slice(open, close));
      for (const value of resolved) {
        const trimmed = value.split("?")[0];
        if (!isControlPlaneApiPath(trimmed)) continue;
        calls.push({
          file: file.file,
          line: file.source.slice(0, match.index).split("\n").length,
          method: method ? method[1] : "GET",
          path: trimmed,
          callee: `${match[1]}.${property}`,
        });
      }
    }
  }
  return calls;
}

// isControlPlaneApiPath keeps the Control Plane's own surface apart from the
// Console BFF's /api/v2/ surface and the external authorities' /api/v1/.
function isControlPlaneApiPath(value: string): boolean {
  return value.startsWith("/api/") && !value.startsWith("/api/v2/") && !value.startsWith("/api/v1/");
}

// enclosingParameters returns the parameters of the function a call site sits
// in, so a parameter name never resolves to a constant of the same name.
function enclosingParameters(functions: Map<string, FunctionInfo>, offset: number): Set<string> {
  let best: FunctionInfo | null = null;
  for (const info of functions.values()) {
    if (offset < info.bodyStart || offset > info.bodyEnd) continue;
    if (!best || info.bodyEnd - info.bodyStart < best.bodyEnd - best.bodyStart) best = info;
  }
  return new Set(best ? best.params.filter((param) => param !== "") : []);
}

// fetchTransport reads the method a fetch call asks for.
function fetchTransport(arguments_: string): { method: string; pathIndex: number } {
  const method = /method\s*:\s*"([A-Z]+)"/.exec(arguments_);
  return { method: method ? method[1] : "GET", pathIndex: 0 };
}

// ------------------------------------------------------------- route match

function segments(path: string): string[] {
  return path.split("/").filter((segment) => segment !== "");
}

function segmentMatches(candidate: string, route: string): boolean {
  if (candidate === route) return true;
  if (!candidate.includes(WILDCARD)) return false;
  if (route.startsWith("{") && route.endsWith("}")) return true;
  const base = candidate.split(WILDCARD)[0];
  return base === route || candidate.startsWith(route) || candidate.endsWith(route);
}

function routeMatches(route: ScannedRoute, client: ClientCall): boolean {
  if (route.method && client.method && route.method !== client.method) return false;
  const routeSegments = segments(route.path);
  const clientSegments = segments(client.path);
  if (routeSegments.length !== clientSegments.length) return false;
  return routeSegments.every((segment, index) => segmentMatches(clientSegments[index], segment));
}

// --------------------------------------------------------------- contract

type Topology = {
  browserApiSurface: {
    rule: string;
    surfaces: { prefix: string; processes: string[]; browserFacing: boolean; evidence: string }[];
    probes?: { method: string; path: string; owner: string; evidence: string }[];
    requirement: string;
  };
};

// --------------------------------------------------------------- the tests

const inventory = controlPlaneRoutes();
const apiRoutes = inventory.routes.filter((route) => route.path.startsWith("/api/"));
const shared = sharedFunctions();
const transports = requestMethods(shared);
const consoleCalls = consoleUiClients(shared, transports);
const toolCallList = toolClients(shared);
const productionCalls = [...consoleCalls, ...toolCallList];
const topology = JSON.parse(readFileSync(TOPOLOGY_PATH, "utf8")) as Topology;
const declaredProbes = topology.browserApiSurface.probes ?? [];

test("every Control Plane route registration is readable from the source", () => {
  assert.deepStrictEqual(
    inventory.unresolved,
    [],
    "a route registered through a pattern this scanner cannot resolve could be a route the ownership check never sees",
  );
});

test("every registered Control Plane /api/ route names the client that calls it", () => {
  const unowned: string[] = [];
  for (const route of apiRoutes) {
    if (route.path === "/api/") continue; // The workspace application proxy, not a Console route.
    const probe = declaredProbes.find((declared) => declared.path === route.path && declared.method === route.method);
    if (probe) continue;
    const clients = productionCalls.filter((client) => routeMatches(route, client));
    if (clients.length === 0) {
      unowned.push(`${route.method || "ANY"} ${route.path} (${path.basename(route.file)}:${route.line})`);
    }
  }
  assert.deepStrictEqual(unowned, [], "every registered route must name its client");
});

test("every client path a client derives exists as a registered route", () => {
  const missing: string[] = [];
  for (const client of productionCalls) {
    const known = apiRoutes.some((route) => routeMatches(route, client));
    if (!known) {
      missing.push(`${client.method} ${client.path} (${path.basename(client.file)}:${client.line} via ${client.callee})`);
    }
  }
  assert.deepStrictEqual(missing, [], "a client must not point at a route the Control Plane does not register");
});

test("the installation's declared probes are registered Control Plane routes", () => {
  const unregistered = declaredProbes.filter((probe) => {
    if (probe.owner !== "control-plane") return false;
    return !apiRoutes.some((route) => route.path === probe.path && route.method === probe.method);
  });
  assert.deepStrictEqual(unregistered, [], "a declared probe must be a route the Control Plane registers");
});

test("the declared browser surfaces exist in the source that registers them", () => {
  const browserFacing = topology.browserApiSurface.surfaces.filter((surface) => surface.browserFacing);
  assert.ok(browserFacing.length > 0, "the contract must declare a browser facing surface");
  const undeclared: string[] = [];
  for (const surface of browserFacing) {
    for (const process of surface.processes) {
      const directory = BROWSER_FACE_PROCESS_DIRECTORIES[process];
      if (!directory) continue;
      const registered = registeredPaths(directory);
      if (!registered.some((path) => path.startsWith(surface.prefix))) {
        undeclared.push(`${surface.prefix} (${process} registers no route under it)`);
      }
    }
  }
  assert.deepStrictEqual(undeclared, [], "the contract must not declare a browser surface the source does not register");
});

// registeredPaths reads the routes a process registers.
function registeredPaths(directory: string): string[] {
  return scanRoutes(directory).routes.map((route) => route.path);
}

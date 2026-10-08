import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { isDeepStrictEqual } from "node:util";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const proto = "packages/contracts/proto";
const target = "docs/spec/target";
const go = "packages/contracts/go";

// These are the actual generator inputs, not a synthetic merged contract.
export const generationInputs = [
  `${proto}/generate.sh`,
  `${proto}/generate_public_json_shape.py`,
  `${proto}/generate_event_identity.py`,
  `${proto}/internal.proto`,
  `${proto}/events.json`,
  `${target}/contracts/internal.proto`,
  `${target}/contracts/events.json`,
  `${target}/contracts/publisher-contract.schema.json`,
  `${target}/03_api_contract_complete.yaml`
] as const;

export const generatedOutputs = [
  `${go}/api/internal.pb.go`,
  `${go}/api/internal_grpc.pb.go`,
  `${go}/publicjson/shape_generated.go`,
  `${go}/event_identity.go`
] as const;

export const uncoveredGeneration = [
  "events.json: no upstream generator exists in the repository; production/target copies are structurally compared as sources, NOT regenerated."
] as const;

export type GenerationCommand = { command: string; args: string[]; cwd: string };
export type GenerationRunner = (command: GenerationCommand) => void;

function runGenerator({ command, args, cwd }: GenerationCommand) {
  const result = spawnSync(command, args, {
    cwd,
    env: { ...process.env, PYTHONDONTWRITEBYTECODE: "1" },
    encoding: "utf8",
    timeout: 120_000,
    maxBuffer: 4 * 1024 * 1024
  });
  if (result.error || result.status !== 0) {
    const invocation = args[0] === "-c" ? `${command} <inline proto descriptor check>` : `${command} ${args.join(" ")}`;
    throw new Error(`${invocation} failed: ${result.error?.message ?? result.signal ?? result.status}\n${result.stderr}${result.stdout}`.trim());
  }
}

// Preserve the complete protobuf descriptors, including options/unknown fields.
// Only the target's declared deployment mapping and its qualified type references
// are rewritten. No sourceinfo is requested; comments never enter the comparison.
export const protoDescriptorProgram = String.raw`
from importlib.metadata import PackageNotFoundError, version
from pathlib import Path
import subprocess
import sys

try:
    actual = version("grpcio-tools")
except PackageNotFoundError:
    raise SystemExit("missing grpcio-tools==1.80.0 for proto schema comparison")
if actual != "1.80.0":
    raise SystemExit(f"grpcio-tools version mismatch: expected 1.80.0, got {actual}")
actual = subprocess.check_output([sys.executable, "-m", "grpc_tools.protoc", "--version"], text=True).strip()
if actual != "libprotoc 31.1":
    raise SystemExit(f"protoc version mismatch: expected libprotoc 31.1, got {actual}")

import grpc_tools
from google.protobuf import descriptor_pb2

TARGET_PACKAGE = "opl.cloud.v226"
PRODUCTION_PACKAGE = "opl.cloud.api"
TARGET_GO_PACKAGE = "opl.cloud/contracts/v226;v226"
PRODUCTION_GO_PACKAGE = "opl-cloud/packages/contracts/go/api;api"
REFERENCE_FIELDS = {
    "google.protobuf.FieldDescriptorProto.type_name",
    "google.protobuf.FieldDescriptorProto.extendee",
    "google.protobuf.MethodDescriptorProto.input_type",
    "google.protobuf.MethodDescriptorProto.output_type",
}

def normalize_references(message):
    for field, value in message.ListFields():
        if field.full_name in REFERENCE_FIELDS and value.startswith("." + TARGET_PACKAGE + "."):
            setattr(message, field.name, "." + PRODUCTION_PACKAGE + value[len(TARGET_PACKAGE) + 1:])
        elif field.type == field.TYPE_MESSAGE:
            for child in value if field.is_repeated else [value]:
                normalize_references(child)

def differences(before, after, path="descriptor"):
    fields_before = {field.name: (field, value) for field, value in before.ListFields()}
    fields_after = {field.name: (field, value) for field, value in after.ListFields()}
    result = []
    for name in sorted(fields_before.keys() | fields_after.keys()):
        here = path + "." + name
        if name not in fields_after or name not in fields_before:
            result.append(here + (": production only" if name not in fields_after else ": target only"))
            continue
        field, left = fields_before[name]
        right = fields_after[name][1]
        if field.type != field.TYPE_MESSAGE:
            if left != right:
                result.append(f"{here}: production={left!r}, target={right!r}")
        elif not field.is_repeated:
            result.extend(differences(left, right, here))
        else:
            key = field.message_type.fields_by_name.get("name")
            if key is not None and key.type == key.TYPE_STRING and not key.is_repeated:
                by_left, by_right = {item.name: item for item in left}, {item.name: item for item in right}
                for symbol in sorted(by_left.keys() | by_right.keys()):
                    item_path = here + "[" + symbol + "]"
                    if symbol not in by_right or symbol not in by_left:
                        result.append(item_path + (": production only" if symbol not in by_right else ": target only"))
                    else:
                        result.extend(differences(by_left[symbol], by_right[symbol], item_path))
            else:
                if len(left) != len(right):
                    result.append(f"{here}: production count={len(left)}, target count={len(right)}")
                for index, (a, b) in enumerate(zip(left, right)):
                    result.extend(differences(a, b, f"{here}[{index}]"))
    return result

compiled = []
for index, (source, output) in enumerate(zip(sys.argv[1:3], sys.argv[3:5])):
    source, output = Path(source), Path(output)
    subprocess.run([
        sys.executable, "-m", "grpc_tools.protoc",
        "-I" + str(source.parent), "-I" + str(Path(grpc_tools.__file__).parent / "_proto"),
        "--include_imports", "--descriptor_set_out=" + str(output), str(source),
    ], check=True)
    descriptors = descriptor_pb2.FileDescriptorSet()
    descriptors.ParseFromString(output.read_bytes())
    roots = [file for file in descriptors.file if file.name == source.name]
    if len(roots) != 1:
        raise SystemExit("expected exactly one source file in the descriptor set")
    root = roots[0]
    if index == 1:
        if root.package == TARGET_PACKAGE:
            root.package = PRODUCTION_PACKAGE
            normalize_references(root)
        if root.options.go_package == TARGET_GO_PACKAGE:
            root.options.go_package = PRODUCTION_GO_PACKAGE
    output.write_bytes(descriptors.SerializeToString(deterministic=True))
    compiled.append(descriptors)
details = differences(*compiled)
Path(sys.argv[5]).write_text("\n".join(details) or "serialized descriptor fields differ")
`;

export function verifyProtoSchema(root: string, runner: GenerationRunner = runGenerator) {
  const outputs = [".proto-parity/production.pb", ".proto-parity/target.pb"];
  const details = ".proto-parity/differences.txt";
  mkdirSync(join(root, ".proto-parity"), { recursive: true });
  runner({
    command: process.env.PYTHON || "python3",
    args: ["-c", protoDescriptorProgram, `${proto}/internal.proto`, `${target}/contracts/internal.proto`, ...outputs, details],
    cwd: root
  });
  if (!readFileSync(join(root, outputs[0])).equals(readFileSync(join(root, outputs[1])))) {
    throw new Error(`proto schema differs: ${proto}/internal.proto != ${target}/contracts/internal.proto (only declared package/go_package deployment mapping normalized; comments excluded)\n${readFileSync(join(root, details), "utf8")}`);
  }
}

function verifyEventSources(root: string) {
  const production = `${proto}/events.json`;
  const specification = `${target}/contracts/events.json`;
  const load = (path: string) => JSON.parse(readFileSync(join(root, path), "utf8"));
  if (!isDeepStrictEqual(load(production), load(specification))) {
    throw new Error(`source JSON differs: ${production} != ${specification} (object key order/whitespace ignored; descriptions and array order preserved)`);
  }
}

function bindings(root: string, directory = `${go}/api`): string[] {
  if (!existsSync(join(root, directory))) return [];
  return readdirSync(join(root, directory), { withFileTypes: true }).flatMap((entry) => {
    const path = `${directory}/${entry.name}`;
    return entry.isDirectory() ? bindings(root, path) : entry.name.endsWith(".pb.go") ? [path] : [];
  });
}

/** Run real generators into an empty output tree. Never rewrite checked-in outputs. */
export function verifyGeneratedContracts({
  root = repositoryRoot,
  runner = runGenerator
}: { root?: string; runner?: GenerationRunner } = {}) {
  const temporary = mkdtempSync(join(tmpdir(), "opl-generated-contracts-"));
  const errors: string[] = [];
  const compared: string[] = [];
  try {
    for (const path of generationInputs) {
      mkdirSync(dirname(join(temporary, path)), { recursive: true });
      copyFileSync(join(root, path), join(temporary, path));
    }
    for (const check of [() => verifyProtoSchema(temporary, runner), () => verifyEventSources(temporary)]) {
      try {
        check();
      } catch (error) {
        errors.push(error instanceof Error ? error.message : String(error));
      }
    }

    // Do not seed outputs: a skipped/no-op generator must fail as missing output.
    for (const path of generatedOutputs) mkdirSync(dirname(join(temporary, path)), { recursive: true });
    const commands: GenerationCommand[] = [
      { command: "bash", args: [`${proto}/generate.sh`], cwd: temporary },
      {
        command: process.env.PYTHON || "python3",
        args: [`${proto}/generate_event_identity.py`, `${proto}/events.json`, `${go}/event_identity.go`],
        cwd: temporary
      }
    ];
    // Report each independent chain even if another generator or source parity fails.
    for (const command of commands) {
      try {
        runner(command);
      } catch (error) {
        errors.push(error instanceof Error ? error.message : String(error));
      }
    }
    const outputs = new Set([...generatedOutputs, ...bindings(root), ...bindings(temporary)]);
    for (const path of [...outputs].sort()) {
      const checkedIn = join(root, path);
      const regenerated = join(temporary, path);
      if (!existsSync(checkedIn)) {
        errors.push(`missing checked-in generated output: ${path}`);
      } else if (!existsSync(regenerated)) {
        errors.push(`missing regenerated output (or obsolete checked-in binding): ${path}`);
      } else if (!readFileSync(checkedIn).equals(readFileSync(regenerated))) {
        errors.push(`generated bytes differ: ${path}`);
      } else {
        compared.push(path);
      }
    }
    return { passed: errors.length === 0, errors, compared, uncovered: [...uncoveredGeneration] };
  } finally {
    rmSync(temporary, { recursive: true, force: true });
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    if (process.argv.length !== 2) throw new Error("verify-generated-contracts accepts no arguments");
    const result = verifyGeneratedContracts();
    for (const limitation of result.uncovered) console.log(`NOT COVERED: ${limitation}`);
    for (const path of result.compared) console.log(`MATCH: ${path}`);
    for (const error of result.errors) console.error(`FAIL: ${error}`);
    console.log(`Generated contracts freshness: ${result.passed ? "PASS" : "FAIL"}`);
    process.exitCode = result.passed ? 0 : 1;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}

#!/usr/bin/env python3
"""Generate the public JSON vocabulary from the canonical schema and the wire proto.

Three tables are compiled from the two real sources:

  1. `enumNames`      public enum text for each wire enum value;
  2. `required`       the properties the contract marks required;
  3. name/representation mappings that keep the *contract* spelling and the
     *contract* scalar form authoritative at the public JSON boundary.

The third group exists because protobuf derives its own JSON name from the field
name, and that derivation is not always the spelling the contract publishes. The
money properties are the clear case: protoc turns `monthly_price_usd_micros` into
`monthlyPriceUsdMicros` while the contract publishes `monthlyPriceUSDMicros`. A
lookup built only from protobuf JSON names therefore cannot resolve the published
property at all.

Resolution is deterministic and generated, never hand-listed:

  * a property is matched to a field by normalizing both sides to
    lowercase alphanumerics, which is exactly the equivalence protobuf itself
    applies between `json_name` and the original field name;
  * a property that normalization cannot reach is a hard error unless it is one of
    the explicitly declared divergences below, so an unresolved public property
    can never silently pass generation;
  * an ambiguity (two fields normalizing to the same key) is a hard error.

The scalar form of a 64-bit integer field is likewise read from the contract: it
is emitted as a JSON string only where the contract types it as a decimal string.
"""
import json
import pathlib
import re
import subprocess
import tempfile

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[3]
API_SCHEMA = ROOT / "docs/spec/target/03_api_contract_complete.yaml"
SCHEMA = ROOT / "docs/spec/target/contracts/publisher-contract.schema.json"
PROTO = ROOT / "docs/spec/target/contracts/internal.proto"
OUTPUT = ROOT / "packages/contracts/go/publicjson/shape_generated.go"

# Public properties whose name and wire field name share a fact but not a
# derivation, so normalization cannot connect them. Each entry must cite why the
# two spellings are the same fact; anything else belongs in the contract or the
# proto, not here.
DECLARED_DIVERGENCES = {
    # 03_api_contract_complete.yaml publishes `expectedCurrentDeploymentId` for
    # both Serve version-selection requests, while the wire field is named
    # `expected_current_agent_deployment_id`. 01_domain_ownership_matrix.md gives
    # Serve the single current agent deployment per Workspace, so the two names
    # denote the same fact.
    "UpdateWorkspaceVersionRequest": {"expectedCurrentDeploymentId": "expected_current_agent_deployment_id"},
    "RollbackWorkspaceRequest": {"expectedCurrentDeploymentId": "expected_current_agent_deployment_id"},
}

# The integer types whose contract form is a JSON decimal string.
STRING_INTEGER_KINDS = {"int64", "uint64", "sint64", "fixed64", "sfixed64"}
STRING_CONTRACT_REFS = {"USDMicros", "NonnegativeInt64", "OpaqueId"}

_KIND_BY_NUMBER = {
    1: "double", 2: "float", 3: "int64", 4: "uint64", 5: "int32", 8: "bool",
    9: "string", 11: "message", 12: "bytes", 13: "uint32", 14: "enum",
    15: "sfixed32", 16: "sfixed64", 17: "sint32", 18: "sint64",
}


def normalize(name):
    return re.sub(r"[^a-z0-9]+", "", name.lower())


def descriptor_messages():
    """Compile the wire proto and return message -> {field name: (json name, kind)}."""
    from grpc_tools import protoc
    import grpc_tools

    well_known = pathlib.Path(grpc_tools.__file__).parent / "_proto"
    with tempfile.TemporaryDirectory(prefix="opl-publicjson-") as temp:
        out = pathlib.Path(temp) / "descriptor.bin"
        rc = protoc.main([
            "protoc",
            f"-I{PROTO.parent}",
            f"-I{well_known}",
            f"--descriptor_set_out={out}",
            "--include_imports",
            str(PROTO),
        ])
        if rc != 0:
            raise SystemExit("failed to compile the wire proto for public JSON shape")
        from google.protobuf import descriptor_pb2

        files = descriptor_pb2.FileDescriptorSet()
        files.ParseFromString(out.read_bytes())
        target = [f for f in files.file if f.name.endswith(PROTO.name)]
        if len(target) != 1:
            raise SystemExit(f"expected exactly one {PROTO.name} in the descriptor set")
        messages = {}

        def walk(declared):
            for message in declared:
                messages[message.name] = {
                    field.name: (field.json_name, _KIND_BY_NUMBER.get(field.type, "unknown"))
                    for field in message.field
                }
                walk(message.nested_type)

        walk(target[0].message_type)
        return messages


def contract_scalar_is_string(value):
    ref = value.get("$ref")
    if isinstance(ref, str) and ref.rsplit("/", 1)[-1] in STRING_CONTRACT_REFS:
        return True
    return value.get("type") == "string"


def public_field_tables(definitions, messages):
    """Return (public_to_field, field_to_public, string_scalars) for the boundary."""
    public_to_field = {}
    field_to_public = {}
    string_scalars = {}
    for name, definition in definitions.items():
        if not isinstance(definition, dict) or not definition.get("properties"):
            continue
        fields = messages.get(name)
        if fields is None:
            raise SystemExit(f"contract message {name} has no wire message")
        by_normalized = {}
        for field_name in fields:
            by_normalized.setdefault(normalize(field_name), []).append(field_name)
        declared = DECLARED_DIVERGENCES.get(name, {})
        forward, reverse, scalar = {}, {}, {}
        for prop, value in definition["properties"].items():
            if prop == "kind":
                # The oneof discriminator is carried by the selected object, not by
                # a wire field of the wrapper.
                continue
            exact = [f for f, (js, _) in fields.items() if js == prop]
            candidates = exact or by_normalized.get(normalize(prop), [])
            if len(candidates) > 1:
                raise SystemExit(f"contract property {name}.{prop} normalizes ambiguously to {candidates}")
            if not candidates:
                field_name = declared.get(prop)
                if field_name is None:
                    raise SystemExit(
                        f"contract property {name}.{prop} has no wire field; "
                        "fix the contract, the proto, or declare the divergence explicitly"
                    )
                if field_name not in fields:
                    raise SystemExit(f"declared divergence {name}.{prop} -> {field_name} is not a wire field")
            else:
                field_name = candidates[0]
            json_name = fields[field_name][0]
            if prop != json_name:
                forward[prop] = field_name
                reverse[field_name] = prop
            if fields[field_name][1] in STRING_INTEGER_KINDS and contract_scalar_is_string(value):
                scalar[field_name] = True
        for prop in declared:
            if prop not in definition["properties"]:
                raise SystemExit(f"declared divergence {name}.{prop} is not a contract property")
        if forward:
            public_to_field[name] = forward
        if reverse:
            field_to_public[name] = reverse
        if scalar:
            string_scalars[name] = scalar
    return public_to_field, field_to_public, string_scalars


def go_map(name, table, value_is_bool=False):
    lines = [f"var {name} = map[protoreflect.Name]map[string]{'bool' if value_is_bool else 'string'}{{"]
    for message in sorted(table):
        entries = ", ".join(
            f"{json.dumps(key)}: " + ("true" if value_is_bool else json.dumps(table[message][key]))
            for key in sorted(table[message])
        )
        lines.append(f"\t{json.dumps(message)}: {{{entries}}},")
    lines.append("}")
    return lines


def main():
    definitions = yaml.safe_load(API_SCHEMA.read_text())["components"]["schemas"]
    definitions.update(json.loads(SCHEMA.read_text())["$defs"])
    proto = PROTO.read_text()
    lines = [
        "// Code generated from the canonical API and publisher schemas; DO NOT EDIT.",
        "package publicjson", "",
        'import "google.golang.org/protobuf/reflect/protoreflect"', "",
        "var enumNames = map[protoreflect.FullName]map[protoreflect.EnumNumber]string{",
    ]
    seen_enums = set()
    for name, definition in definitions.items():
        properties = list(definition.get("properties", {}).items())
        if definition.get("enum") and re.search(r"enum " + name + r"Enum \{", proto):
            properties.append(("", definition))
        for field, value in properties:
            if value.get("type") == "array":
                value = value.get("items", {})
            values = value.get("enum", [])
            if not values or not all(isinstance(item, str) for item in values):
                continue
            enum_name = name + (field[0].upper() + field[1:] if field else "") + "Enum"
            if enum_name in seen_enums:
                continue
            seen_enums.add(enum_name)
            match = re.search(r"enum " + enum_name + r" \{(.*?)\n\}", proto, re.S)
            if match is None:
                raise ValueError(f"Missing wire enum: {enum_name}")
            numbers = {
                key.rsplit("_ENUM_", 1)[1]: int(number)
                for key, number in re.findall(r"(\w+)\s*=\s*(\d+);", match.group(1))
            }
            pairs = []
            for public_value in values:
                key = re.sub("[^A-Za-z0-9]+", "_", public_value).upper().strip("_")
                pairs.append(f"{numbers[key]}: {json.dumps(public_value)}")
            lines.append('"opl.cloud.api.' + enum_name + '": {' + ", ".join(pairs) + "},")
    lines += ["}", "", "var required = map[protoreflect.Name]map[string]bool{"]

    seen_messages = set()

    def required_properties(name, definition):
        if name in seen_messages:
            return
        seen_messages.add(name)
        required = definition.get("required", [])
        if required:
            pairs = ", ".join(json.dumps(field) + ": true" for field in required)
            lines.append(json.dumps(name) + ": {" + pairs + "},")
        for field, value in definition.get("properties", {}).items():
            if value.get("type") == "object" and value.get("properties"):
                required_properties(name + field[0].upper() + field[1:], value)

    for name, definition in definitions.items():
        required_properties(name, definition)
    lines.append("}")

    public_to_field, field_to_public, string_scalars = public_field_tables(definitions, descriptor_messages())
    if not public_to_field or not field_to_public or not string_scalars:
        raise SystemExit("expected at least one name divergence and one string integer field")
    lines += [
        "",
        "// publicToField resolves a public property name to its wire field name where",
        "// protobuf's own JSON name differs from the spelling the contract publishes.",
        "// The public contract spelling is authoritative at this boundary.",
    ]
    lines += go_map("publicToField", public_to_field)
    lines += [
        "",
        "// fieldToPublic is the same mapping read the other way, so an encoded response",
        "// carries the contract property name rather than the protobuf JSON name.",
    ]
    lines += go_map("fieldToPublic", field_to_public)
    lines += [
        "",
        "// stringScalars marks 64-bit integer fields the contract types as a JSON decimal",
        "// string. Any other 64-bit integer field keeps the numeric JSON form.",
    ]
    lines += go_map("stringScalars", string_scalars, value_is_bool=True)

    OUTPUT.write_text("\n".join(lines) + "\n")
    subprocess.run(["gofmt", "-w", str(OUTPUT)], check=True)


if __name__ == "__main__":
    main()

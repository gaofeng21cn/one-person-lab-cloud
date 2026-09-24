#!/usr/bin/env python3
"""Generate the public JSON vocabulary from its schema and generated wire enums."""

import json
import pathlib
import re
import subprocess

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[3]
API_SCHEMA = ROOT / "docs/spec/target/03_api_contract_complete.yaml"
SCHEMA = ROOT / "docs/spec/target/contracts/publisher-contract.schema.json"
PROTO = ROOT / "docs/spec/target/contracts/internal.proto"
OUTPUT = ROOT / "packages/contracts/go/publicjson/shape_generated.go"


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
    OUTPUT.write_text("\n".join(lines) + "\n")
    subprocess.run(["gofmt", "-w", str(OUTPUT)], check=True)


if __name__ == "__main__":
    main()

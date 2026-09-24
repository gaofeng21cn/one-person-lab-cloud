#!/usr/bin/env python3
"""Generate the public JSON vocabulary from its schema and generated wire enums."""

import json
import pathlib
import re
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[3]
SCHEMA = ROOT / "docs/spec/target/contracts/publisher-contract.schema.json"
PROTO = ROOT / "docs/spec/target/contracts/internal.proto"
OUTPUT = ROOT / "packages/contracts/go/publisherjson/shape_generated.go"


def main():
    schema = json.loads(SCHEMA.read_text())
    proto = PROTO.read_text()
    lines = [
        "// Code generated from docs/spec/target/contracts/publisher-contract.schema.json; DO NOT EDIT.",
        "package publisherjson", "",
        'import "google.golang.org/protobuf/reflect/protoreflect"', "",
        "var enumNames = map[protoreflect.FullName]map[protoreflect.EnumNumber]string{",
    ]
    for name, definition in schema["$defs"].items():
        for field, value in definition.get("properties", {}).items():
            if value.get("type") == "array":
                value = value.get("items", {})
            values = value.get("enum", [])
            if not values or not all(isinstance(item, str) for item in values):
                continue
            enum_name = name + field[0].upper() + field[1:] + "Enum"
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

    def required_properties(name, definition):
        required = definition.get("required", [])
        if required:
            pairs = ", ".join(json.dumps(field) + ": true" for field in required)
            lines.append(json.dumps(name) + ": {" + pairs + "},")
        for field, value in definition.get("properties", {}).items():
            if value.get("type") == "object" and value.get("properties"):
                required_properties(name + field[0].upper() + field[1:], value)

    for name, definition in schema["$defs"].items():
        required_properties(name, definition)
    lines.append("}")
    OUTPUT.write_text("\n".join(lines) + "\n")
    subprocess.run(["gofmt", "-w", str(OUTPUT)], check=True)


if __name__ == "__main__":
    main()

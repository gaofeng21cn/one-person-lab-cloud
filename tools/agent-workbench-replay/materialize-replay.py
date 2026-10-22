"""Materialize recorded operations locally. Never SSH, build, run Docker, or qualify."""
from pathlib import Path, PurePosixPath
import argparse
import sys
import hashlib
import json
import shutil
import tarfile

ROOT = Path(__file__).resolve().parent
PROVENANCE = json.loads((ROOT / "provenance.json").read_text())


def main():
    if sys.version_info < (3, 12):
        raise SystemExit("Python 3.12+ is required for safe archive extraction and dev operations")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inputs", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--remote-root", required=True)
    parser.add_argument("--model-config-on-dev", required=True)
    parser.add_argument("--dev-registry", default="127.0.0.1:5000")
    parser.add_argument("--dry-run", action="store_true", help="Verify exact inputs and compile/save scripts; omit input copy and source extraction")
    args = parser.parse_args()
    output = args.output.resolve()
    remote = PurePosixPath(args.remote_root)
    import re
    if remote.name == "agent-package-dev-20261021t182745z" or not remote.is_absolute() or ".." in remote.parts or not re.fullmatch(r"agent-package-dev-[a-z0-9-]+", remote.name):
        parser.error("Use a new absolute remote root named agent-package-dev-<unique-id>")
    if not PurePosixPath(args.model_config_on_dev).is_absolute():
        parser.error("Model configuration must be an absolute path on the dev host")
    if not re.fullmatch(r"127[.]0[.]0[.]1:[0-9]+", args.dev_registry) or not 0 < int(args.dev_registry.rsplit(":", 1)[1]) < 65536:
        parser.error("Development artifact Registry must be IPv4 loopback with an explicit port")
    if output.exists() or output.is_relative_to(ROOT.parents[1]):
        parser.error("Output must be a new directory outside this checkout")
    expected = {"cloud-source.tar.gz": PROVENANCE["cloudSourceArchive"]["sha256"], "ibd-evidence-assistant-v2-0.2.0-upload-package.zip": PROVENANCE["package"]["originalSha256"], "agent-webui.oci.tar": PROVENANCE["externalWebuiOci"]["sha256"]}
    expected.update({name: info["sha256"] for name, info in PROVENANCE["publicInputs"].items()})
    for name, sha in expected.items():
        if "sha256:" + hashlib.sha256((args.inputs / name).read_bytes()).hexdigest() != sha:
            raise ValueError("Input SHA-256 mismatch: " + name)
    substitutions = {"__RUN_ROOT__": str(remote), "__MODEL_CONFIG_ON_DEV__": args.model_config_on_dev, "__DEV_REGISTRY__": args.dev_registry}
    generated = {}
    for operation in PROVENANCE["operations"]:
        path = operation["path"]
        raw = (ROOT / path).read_bytes()
        if "sha256:" + hashlib.sha256(raw).hexdigest() != operation["publicSha256"]:
            raise ValueError("Adapted operation changed: " + path)
        text = raw.decode()
        for token, value in substitutions.items():
            text = text.replace(repr(token), repr(value))
        if any(token in text for token in substitutions):
            raise ValueError("Unresolved operation parameter: " + path)
        compile(text, path, "exec")
        generated[path] = text
    output.mkdir(parents=True, mode=0o700)
    (output / "operations").mkdir()
    for path, text in generated.items():
        (output / path).write_text(text)
    (output / "commands.txt").write_text("\n".join("python3 " + path for path in generated) + "\n")
    if not args.dry_run:
        (output / "inputs").mkdir()
        for name in expected:
            shutil.copyfile(args.inputs / name, output / "inputs" / name)
        (output / "source").mkdir()
        with tarfile.open(output / "inputs/cloud-source.tar.gz") as source:
            if source.pax_headers.get("comment") != PROVENANCE["historicalSourceSha"]:
                raise ValueError("Source archive commit binding mismatch")
            source.extractall(output / "source", filter="data")
        for name in ["evidence", "bin"]:
            (output / name).mkdir()
    result = {"kind": "new_local_materialization_check", "historicalSourceSha": PROVENANCE["historicalSourceSha"], "dryRun": args.dry_run, "verifiedInputFiles": len(expected), "compiledOperations": len(generated), "remoteActionsExecuted": 0, "businessReplayExecuted": False}
    (output / "materialization.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result))


if __name__ == "__main__":
    main()

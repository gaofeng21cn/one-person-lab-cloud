"""Reconstruct exact historical inputs outside the checkout; no remote operations."""
from pathlib import Path
import argparse
import sys
import hashlib
import io
import json
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parent
PROVENANCE = json.loads((ROOT / "provenance.json").read_text())


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def require_bytes(data, expected):
    if digest(data) != expected:
        raise ValueError("Input SHA-256 mismatch; refusing changed historical input")
    return data


def main():
    if sys.version_info < (3, 12):
        raise SystemExit("Python 3.12+ is required for safe archive extraction and dev operations")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--webui-oci", type=Path, required=True)
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--original-package", type=Path)
    group.add_argument("--historical-state-root", help="Original private state root; never saved in public material")
    args = parser.parse_args()
    output = args.output.resolve()
    checkout = ROOT.parents[1]
    if output.is_relative_to(checkout) or output.exists():
        parser.error("Output must be a new directory outside this checkout")
    package_info = PROVENANCE["package"]
    if args.original_package:
        package = args.original_package.read_bytes()
    else:
        public = require_bytes((ROOT / "inputs/package.public.zip").read_bytes(), package_info["publicSha256"])
        restored = io.BytesIO()
        with zipfile.ZipFile(io.BytesIO(public)) as source, zipfile.ZipFile(restored, "w") as target:
            for entry in source.infolist():
                body = source.read(entry.filename).replace(package_info["publicToken"].encode(), args.historical_state_root.encode())
                target.writestr(entry, body, compress_type=entry.compress_type, compresslevel=6)
        package = restored.getvalue()
    require_bytes(package, package_info["originalSha256"])
    webui = require_bytes(args.webui_oci.read_bytes(), PROVENANCE["externalWebuiOci"]["sha256"])
    source = subprocess.check_output(["git", "-C", str(args.repository), "archive", "--format=tar", PROVENANCE["historicalSourceSha"]])
    require_bytes(source, PROVENANCE["cloudSourceArchive"]["tarSha256"])
    archive = subprocess.run(["gzip", "-n", "-6"], input=source, capture_output=True, check=True).stdout
    require_bytes(archive, PROVENANCE["cloudSourceArchive"]["sha256"])
    output.mkdir(parents=True, mode=0o700)
    for name, data in {"cloud-source.tar.gz": archive, "ibd-evidence-assistant-v2-0.2.0-upload-package.zip": package, "agent-webui.oci.tar": webui}.items():
        (output / name).write_bytes(data)
    for name, expected in PROVENANCE["publicInputs"].items():
        data = require_bytes((ROOT / "inputs" / name).read_bytes(), expected["sha256"])
        (output / name).write_bytes(data)
    print(json.dumps({"prepared": True, "sourceSha": PROVENANCE["historicalSourceSha"], "packageSha256": digest(package), "sourceArchiveSha256": digest(archive), "webuiArchiveSha256": digest(webui)}))


if __name__ == "__main__":
    main()

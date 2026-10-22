# Historical Agent Package → OCI → Workspace dev replay

The historical run used Cloud source `3cefc17ea9e065a880039f416721522512ea5215`
on October 21, 2026 UTC (October 22 in Asia/Shanghai). That source is distinct
from the PR revision carrying these materials. The public
[evidence projection](../evidence/agent-workbench/dev-delivery.json) selects
verbatim fields using JSON Pointers and binds each original file by SHA-256 and
byte length. It is a sanitized projection, **not an original receipt**. Original
proof remains with the dev artifact custodian; its hashes alone do not make the
unpublished bytes independently available or attest this PR's execution.

The small actual [Build log projection](../evidence/agent-workbench/build-owner-retry-1.projection.log)
and [Workspace log projection](../evidence/agent-workbench/workspace-test.projection.log)
are included for review. Their only edit replaces the literal original run root
with `${RUN_ROOT}`. Original/projected hashes, sizes and replacement counts are
in `dev-delivery.json`; loopback ports are preserved as historical identifiers
of the removed Registry. These are projected execution logs, not original bytes.

The run completed real Package upload, BuildKit construction, immutable OCI
readback, CapabilityVersion registration, Workspace/Serve/Fabric Local-Docker
readiness and authenticated native HTTP `/api/send-message` with a canonical
completed Agent turn. External identity/membership and Runtime/WebUI admission
were fixtures. Build and Workspace used separate stores joined by the exact
schema-2 handoff, rather than one continuously installed catalog.

| Identity | Historical value |
| --- | --- |
| Package ZIP | `sha256:1befa906cdfb4f343beb0f615df36588a03bda53abefdfd3a90f7a171bba2b19` |
| Runtime base | `sha256:8efab2844b52c66bcaa3da05637abc3cbdb0c3b29364718839b121284711de92` |
| Original extracted WebUI manifest | `sha256:88790a22db669daa917ccf6a2f1d7650235ebd70f810696ce237e4fc39edcb69` |
| Registered WebUI after Docker reexport | `sha256:e2bbb23bb17125cfdccfb1928a005f22b9aef48b28c043a2f642d0b3a427b6f3` |
| Final application OCI manifest | `sha256:77ecf97d73b178993295546998111aa31226ae89c21e5a36b002ffb02a60af0b` |
| Immutable Build handoff file | `sha256:d0eab9ff36cd437c997644ee568bea6338fe2603ccf8c3f21141752d82c0c9d1` |
| Final OCI archive, 485,120,000 bytes | `sha256:d6df82e27113a9c1b8ba632f0d33078ddcccdbfd5faac015d87c6469f0e48edc` |

These are different objects. The final OCI archive was retained on dev; it is
not included here and is not claimed to be currently published. Loopback
Registry references in the historical projection identify an ephemeral Registry
removed by cleanup. They are not usable download endpoints. Docker's merged
filesystem readback verified four WebUI and 89 carrier files; it supersedes
the earlier exporter's layer-accumulation file-content check. Its manifest/blob
and archive hashes remain separate evidence.

The extracted WebUI's earlier browser/native-runtime evidence used Runtime
`c9ec31c…`, not this run's `8efab284…` base. That earlier browser result does
not establish browser sending from the final application OCI. This run used
`LOCAL_NO_CHARGE` with an unactivated order. Customer deletion, Ledger deletion
receipt and refund were unexecuted; recorded resource absence is harness cleanup.
PR #1013 records separate deletion evidence. This historical scope does not
negate that evidence or change global status. No paid activation, production,
Tencent qualification, Candidate/Instance qualification, Gateway handover or
guideline retrieval is established.

## Supplied inputs and exact reconstruction

The [replay directory](../../tools/agent-workbench-replay/) contains two local
preparation tools, seven adapted historical operations, the small Runtime
publisher contract and Package schema, a static-file receipt projection, cached
tool identities and a public Package input. [provenance.json](../../tools/agent-workbench-replay/provenance.json)
binds original operation bytes separately from adapted bytes. It is provenance
metadata, not a qualification receipt or a new product contract.

The original ZIP declared `Apache-2.0` and author `one-person-lab` in
`manifest.json`. Its three files containing personal state paths were replaced
with the literal `__SOURCE_STATE_ROOT__` token. The public ZIP is 219,899 bytes;
the original is 219,963 bytes. Exactly 87 of 90 entries retain their contents.
Entry order and ZipInfo metadata are preserved, with DEFLATE level 6. The
per-entry before/after hashes, sizes and five replacement occurrences are in
`provenance.json`. This public ZIP is a derivative input, not the historically
uploaded ZIP, and the historical final-filesystem hashes refer to the original.
Existing packaging-provenance statements inside the ZIP describe its prior
construction, not a new attestation of the derivative.

Exact input preparation accepts either the retained original ZIP or the
operator-supplied **original** state-root string. With the latter, it reverses
the token substitution and refuses to proceed unless the original ZIP SHA-256
matches. The private string is deliberately absent from this repository.
Reconstruction has been checked byte-for-byte locally; it is not a new runtime
acceptance. Do not commit reconstructed inputs or generated plans, which can
contain private paths.

The Cloud archive is generated from the historical Git commit using exactly
`git archive --format=tar <sha> | gzip -n -6`. Both the uncompressed tar hash and
original gzip hash must match; differing Git/gzip output is an error, not an
accepted substitute. No whole source archive is committed.

Required external input: the retained original WebUI OCI tar, 243,200 bytes,
`sha256:8bd43515956c7bdd50bc2cdd75ebecb7e36c687200c3001aeb7b76cc9e9405ec`.
Its exact availability and manifest are recorded in `provenance.json`; no public
artifact location is supplied. Obtain these exact bytes from the dev custodian.
Rebuilding from the WebUI source would create a different artifact and requires
new qualification. The final 485 MB OCI is historical output, not a replay input.

From the repository root, use operator-supplied paths outside the checkout:

```sh
python3 tools/agent-workbench-replay/prepare-inputs.py \
  --repository "$CLOUD_REPOSITORY" \
  --output "$NEW_PRIVATE_INPUT_DIRECTORY" \
  --webui-oci "$RETAINED_WEBUI_OCI" \
  --original-package "$RETAINED_ORIGINAL_PACKAGE_ZIP"
```

Alternatively replace `--original-package` with
`--historical-state-root "$ORIGINAL_STATE_ROOT"`. Wrong original paths or changed
ZIP compression produce a hash failure. Both modes export the same historical
source and Package bytes. No private source directory from the old bundle is
required.

## Existing-dev prerequisites

This is an **existing-dev reproduction recipe**, not a clean-host or clean-checkout
runnable qualification. A public checkout alone lacks the immutable WebUI OCI,
compiler and Runtime/dev tool caches, and authorized model input. No acquisition
or publication of those private dev artifacts is implied.

Use a dedicated, authorized Linux amd64 dev environment with Python 3.12+,
Docker/Buildx, passwordless `sudo`, ext4 project-quota/loop support, normal Go
module access or populated module cache, and space for the sparse 12 GiB quota
filesystem plus build outputs. Preserve the exact cache identities in
[tooling.json](../../tools/agent-workbench-replay/inputs/tooling.json). The compiler
is a retained dev image: the observed compiler version was Go 1.26.8 regardless
of other cached tag names. This recipe does not rebuild or replace that compiler.
The Runtime's registry name in the unchanged publisher contract is provenance;
`OPL_BUILD_TEST_RUNTIME_LOCAL=1` selects its retained local bytes. No Tencent
provider, production endpoint or real billing operation is part of this recipe.

Before any operation, require all cached image IDs to match:

```sh
python3 - "$REPLAY_ROOT" <<'PY_CHECK_CACHE'
from pathlib import Path
import json, subprocess, sys
p = Path(sys.argv[1])
for image, expected in json.loads((p / 'inputs/tooling.json').read_text())['images'].items():
    actual = json.loads(subprocess.check_output(['docker', 'image', 'inspect', image]))[0]['Id']
    assert actual == expected['imageId'], 'Required cached image changed: ' + image
assert subprocess.run(['docker', 'image', 'inspect', 'postgres:16'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0
assert subprocess.run(['sudo', '-n', 'true']).returncode == 0
PY_CHECK_CACHE
```

`postgres:16` must initially be absent; the historical harness temporarily aliases
retained PostgreSQL bytes and cleanup restores absence. The artifact Registry
selected by `--dev-registry` must already be running on dev loopback, and accept
writes only in this run's unique `opl-dev/<run-name>/webui` repository. It is
separate from the temporary output Registry created by the Build test. Pin its
existing server to the recorded Registry image bytes. The historical preview
container is not an application dependency; the old preview-preservation/export
script is intentionally not ported. Retain an initial inventory of all dev
container IDs/states and verify it after cleanup.

The model-config path must already be authorized for the dev native Agent. It
contains the selected provider/model credential; supply it only on dev. The
operation copies it into a mode-0700 private directory with a mode-0600 file.
The disposable PostgreSQL password is generated per replay and stored privately.
Neither credential is committed. Protect generated logs and outputs until the
operator checks them against the actual credential values; these operations do
not certify new evidence as sanitized.

## Materialize and execute

Materialization is local only: it never SSHs, starts Docker, runs tests or exports
historical-looking receipts. Choose a new remote directory named
`agent-package-dev-<unique-id>` and a new local output directory outside the
checkout. The remote directory must not already exist, must have no symlink
ancestor, and must never be the historical run directory.

```sh
python3 tools/agent-workbench-replay/materialize-replay.py \
  --inputs "$NEW_PRIVATE_INPUT_DIRECTORY" \
  --output "$NEW_PRIVATE_PLAN_DIRECTORY" \
  --remote-root "$NEW_REMOTE_RUN_DIRECTORY" \
  --model-config-on-dev "$AUTHORIZED_MODEL_CONFIG_ON_DEV" \
  --dev-registry 127.0.0.1:5000 \
  --dry-run
```

Dry-run verifies all exact inputs, compiles seven parameterized scripts and
writes `commands.txt` plus a clearly labeled **new local materialization check**.
It omits input copying and source extraction. Use another new local output
and omit `--dry-run` to create the complete input/source/operation directory.
Transfer that directory to the chosen new remote path using your authorized dev
transport; refuse an existing destination. Do not run the generated operations
on the local workstation. Set `REPLAY_ROOT` to the new remote path, perform the
preflight above, save the initial dev inventory, change directory with `cd "$REPLAY_ROOT"`,
and execute the generated `commands.txt` in order on dev, stopping on the first failure:

1. `compile-remote-retry.py`: compile Build/Workspace test binaries, Fabric and
   Serve from the exact archive using the recorded compiler image ID.
2. `prepare-build-input.py`: upload and read back the exact original WebUI
   manifest, substitute only the disposable `webui/cloud-agent/Dockerfile` input,
   create the temporary PostgreSQL alias and verify retained BuildKit/frontend
   cache IDs. Product Go source remains unchanged. No mutable tooling pull is used.
3. `run-build-retry-1.py`: real upstream Build test; fixed working directory,
   schema-2 immutable handoff and fresh result/log files.
4. `export-final-oci.py`: verify manifest/config/layers, export the new OCI and
   pull its exact digest from the run's output Registry for Workspace use.
   Its accumulated layer file checks are intermediate only.
5. `prepare-workspace.py`: dedicated PostgreSQL, quota filesystem and private
   model input. Creates only this run's directory/resources.
6. `run-workspace.py`: real downstream Workspace/Serve/Fabric test with the
   immutable handoff imported into its separate Capability fixture store;
   native HTTP response, not final-OCI browser send.
7. `verify-final-filesystem.py`: Docker merged filesystem proof for the exact
   **new** output image and original input files. Removes its own non-running
   verification container in `finally`.

Fresh run identifiers, timestamps, output Registry address, Build handoff and
possibly OCI digest differ from history. Evaluate new receipts from actual
execution; do not copy historical IDs, pass fields, timestamps or hashes into
them. The materializer's result reports `businessReplayExecuted: false` because
materialization itself does not execute this sequence.

## Bounded cleanup and evidence handling

The original `cleanup-and-export.py`, `restore-tooling-tag.py` and
`final-export-evidence.py` are not distributed as executable cleanup. They assumed
fixed directory ancestry, a particular preview and shared-host state. Cleanup
is an explicit dev operator step after success or failure:

- Read the actual new `workspace-preflight.json` and `build-handoff.json` to
  identify the dedicated PostgreSQL and temporary output Registry containers.
  Inspect their exact IDs, labels and loopback bindings; prove neither ID belongs
  to the saved initial inventory. Remove only those verified containers with
  `docker rm -f -v <exact-id>`. Do not stop or remove the pre-existing artifact
  Registry or other dev services. If failure preceded a receipt, inspect the
  generated operation and exact run labels before selecting its resource.
- The fixed Workspace test owns its own temporary runtime cleanup. Read back
  containers, networks and volumes for the actual new Workspace ID. A residual
  resource requires diagnosis and exact ownership proof; no global prune or
  customer deletion/refund API is part of cleanup.
- Ensure run processes have exited and no active container mounts the run
  directory. Unmount only `$REPLAY_ROOT/storage`; inspect loop devices and
  detach only the exact backing file `$REPLAY_ROOT/quota.img`. Then remove that
  image and the run's `private` and `tmp` directories. Keep model configuration
  at its original authorized source untouched. Do not delete the run directory,
  final OCI, receipts or cache images.
- Require the temporary `postgres:16` tag to resolve to
  `sha256:301bcb60b8a3ee4ab7e147932723e3abd1cef53516ce5210b39fd9fe5e3602ae`
  before removing **only that alias** with
  `docker image rm --force --no-prune postgres:16`. Verify tag absence, retained
  image bytes and every initial container ID/state. Keep BuildKit/frontend caches.
- Scan actual model/other credential values out of any proposed public output.
  Project only needed fields, retain original hashes separately, and label the
  projection. Never overwrite historical action records. Resource absence after
  these steps proves harness cleanup only.

The public change's local input/materialization validation is recorded in
[public-material-validation.json](../evidence/agent-workbench/public-material-validation.json).
It establishes public-data bindings and script preparation, with no additional
dev business replay or current-revision runtime qualification.

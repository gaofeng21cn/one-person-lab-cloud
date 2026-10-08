# Scoped Development

This directory owns development-task authorization and the restricted file-tool
entry. It does not own product status, business fields or deployment acceptance.
Current product evidence remains in `docs/status.md`; open outcomes remain in
`docs/roadmap.md`. This task implements development controls, not a running Cloud
installation.

## What is enforced

`tools/dev-session.ts` offers only four stdio MCP tools:

- `dev_context`: reads the mandatory current inputs and returns their exact
  content, file hashes, current Git HEAD, task hash, next action, blockers,
  authorized paths and acceptance commands.
- `dev_read`: reads one authorized regular file and its hash.
- `dev_search`: searches a literal in explicit authorized paths, with limits on
  file count, bytes and results. It never accepts the repository root, wildcard
  paths, traversal or an unrestricted shell command.
- `dev_write`: replaces one authorized file only when its current SHA-256 still
  matches the value the caller supplied. New files require `absent` and an
  existing parent directory. The active task file itself cannot be written.

File operations are rejected before `dev_context`. HEAD, task or mandatory
input changes revoke admission; the worker must receive current context again.
Symbolic links and hard-linked files are refused. Unknown arguments and task
fields fail validation; budgets fail explicitly rather than silently truncating
results. The broker is synchronous and assumes its directory is not concurrently
replaced by an adversarial host process; it is not an OS filesystem sandbox.

These checks establish context **delivery**, not understanding, compliance with
all prose, or business correctness. The agent can still make a wrong change
inside an authorized file. Focused behavior tests remain necessary.

## Host integration is part of enforcement

Start the server from the repository root:

```sh
npm --silent run dev:tools -- development/tasks/development-gates.json
```

Configure that process as an MCP stdio server in the development runner. Use
`node tools/dev-session.ts serve <task.json>` directly, or the silent npm command
above: npm banners on stdout would corrupt the stdio protocol. The
restricted worker must have **only these repository tools**: no native shell,
unrestricted file editor, alternate MCP filesystem, or direct repository mount
outside the host's authorized scope. The trusted host retains build/test and
policy administration. An MCP server cannot remove tools granted by its host.
Adding this server alongside an unrestricted shell is **not enforcement**.
There is no automatic modification of Codex, Claude Code, global settings or an
existing chat in this change. Client-specific hook support is not assumed.

The host chooses a task before launching the worker and protects the task and
runner from worker writes. Tools return errors on denied operations. Git/CI can
check resulting changes, but cannot retroactively stop reads made through an
unrestricted tool. If the host cannot disable those tools, report the lane as
unrestricted, not protected.

## Current state is an observation, not another status writer

```sh
npm run dev:context -- development/tasks/development-gates.json
```

This generates `.runtime/development/development-gates.json` (ignored by Git).
It binds the selected task and mandatory input bytes to the observed HEAD. A
new worker must call `dev_context` again; a saved file is not a reusable admission
token. No date, commit or passing unit test is upgraded into a runtime claim.
Task `nextAction` is the host's instruction, not a generated diagnosis of Cloud
or a claim that external blockers have been resolved.

The mandatory full inputs are `AGENTS.md`, `docs/status.md` and
`docs/roadmap.md`; additional owner inputs may select explicit inclusive line
ranges. Each range is validated, and the hash covers the entire source file.
Do not omit current evidence to make the context shorter. If the context exceeds
its budget, select the necessary sections of additional owner documents.

## Fixed task fields and scope verification

`development/development-contract.json` is the owner registry and field contract.
`parseTask()` in `tools/dev-session.ts` requires the fixed task fields:
`schemaVersion`, `id`, `objective`, `domainOwner`, `primaryModule`,
`canonicalContract`, `callers`, `nextAction`, `blockers`, `context`, `readPaths`,
`writePaths`, `currentState`, `flow`, `evidence`, and `completionEvidence`. The
DDD owner must own the primary module, canonical contract and every write path;
flow stages name their owner and terminal acceptance stage. A task that omits an
owner, caller, current-state anchor, terminal stage or evidence layer is refused.
Paths are exact files or directory prefixes ending in `/`; `.` and globs are not
permissions. Acceptance commands are displayed for the trusted host, never
executed by the worker tool. This avoids granting arbitrary execution through a
model-edited test file.

Once a task has been approved in the base commit:

```sh
npm run verify:dev-scope -- development/tasks/example.json <exact-base-sha>
```

The verifier checks committed, staged, unstaged and non-ignored untracked
changes against the **base commit's** task. It rejects changes to that task and
cannot be tricked by an edited task expanding its own permissions. Renames are
checked as source deletion plus destination addition. Ignored files are not a
Git authorization surface; the restricted broker separately denies `.git` and
`.runtime` access. The verifier does not promise to detect reads or transient
writes reverted before the check.

This is a callable gate, not an assertion that GitHub branch protection is
configured. A trusted integration runner must supply the reviewed base and task
and run the trusted verifier, not a worker-modified copy. A new task is approved
as a separate policy change before the scoped implementation starts. The
initial rollout adds its first task; it does not pretend that task existed in
its parent commit. Independent tasks can use different files concurrently;
shared contract integration remains a real synchronization point.

## Closed-loop evidence

The worker cannot write `docs/status.md`, `docs/roadmap.md`, the task receipt or
its machine state. After the trusted host runs the task's stages, it writes a
fixed receipt and invokes:

```sh
npm run dev:publish -- development/tasks/development-gates.json /absolute/path/to/receipt.json
```

`publish` requires the receipt's task id, exact source SHA, evidence layer,
result, commands and changed paths. It rejects changed paths outside the
approved DDD owner scope, writes the immutable receipt under the task's evidence
path, updates `development/state/current.json`, and regenerates a bounded state
block in both `docs/status.md` and `docs/roadmap.md`. A `blocked` receipt is a
valid outcome and remains visible; it is not converted into success. This is a
trusted-host operation, not an agent claim and not a deployment receipt.

"走闭环" therefore has one machine path: current-state delivery -> scoped owner
work -> fixed acceptance stages -> trusted receipt -> current-state projection.
A product capability still needs its own runtime or Instance evidence layer.

## Generated fields

```sh
npm run verify:generated-contracts
```

The generated-contract verifier regenerates the explicitly owned production
outputs in an isolated temporary tree and compares exact bytes. Missing pinned
tools, mismatched duplicate sources and changed outputs fail. The existing
canonical proto and publisher schema remain the field owners; this directory
does not introduce a second field catalog. New generated outputs must join this
verifier through their real generator and a negative-control test. The gate is
callable now, but it is not yet a blocking required CI check: the production
proto and the target specification still carry an owner-reconciled semantic
drift (see the `contracts` owner), so wiring it as a required check would red the
shared `main`. Handwritten
business fields and semantic invariants are **not automatically frozen** by
adding a checksum or this documentation.

## Verification

```sh
npm run test:development-gates
```

Tests exercise denial before context, stale context, root searches, scope
violations, symbolic-link escapes, concurrent edit conflicts, schema errors,
self-expanded task permissions and a real stdio process. Generated-contract
checks have separate regeneration/byte-drift tests. They prove only the named
controls; they do not replace the Default App user journey or Instance readback.

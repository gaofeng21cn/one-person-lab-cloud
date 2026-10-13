# Deprecated: Development Framework

**Status:** DEPRECATED as of 2026-10-13
**Reason:** Over-engineered for single-developer workflow

## Background

This framework was designed to solve:
- Low code review quality
- Multi-branch conflicts

It required:
- `docs(plan)` before each change
- `docs(evidence)` after each change
- Scope verification and rebinding

## Why Deprecated

The framework assumed a multi-person team with strict compliance requirements, but the actual context is:
- Single developer
- Need for rapid iteration
- Self-review scenario

The overhead of plan/evidence/rebind became larger than the changes themselves, blocking business closure progress.

## Replacement

See `docs/COMMIT_GUIDELINES.md` for the new lightweight approach:
- Clear commit messages (replacing plan/evidence)
- Small, frequent commits
- Maximum 3 active branches
- Tools over process

## Archive

Framework code and documentation preserved in:
- `docs/evidence/` (archived)
- Git history (all plan/evidence commits remain)

Do not create new plan/evidence documents going forward.

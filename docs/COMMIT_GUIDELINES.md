# Commit Guidelines

## Philosophy

**Small steps, clear intent, fast iteration.**

No heavy process. Let tools do the checking. Keep commits atomic and messages clear.

## Commit Message Format

```
<type>(<scope>): <what changed>

Problem: <what was broken or missing>
Solution: <how you fixed it>
Impact: <which components affected>
```

### Example

```
fix(ui): prevent double-submit in workspace launch

Problem: User could click submit twice, creating duplicate workspaces
Solution: Add pendingSubmit state guard in useWorkspaceLaunchController
Impact: apps/console-ui/src/app/use-workspace-launch-controller.ts only
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code restructure, no behavior change
- `docs`: Documentation only
- `chore`: Build, deps, tooling

## Before Commit Checklist

Ask yourself 3 questions:

1. **What changed?** (one thing only)
2. **Why?** (problem being solved)
3. **Impact?** (which files/components)

Answer them in the commit message.

## Pre-Commit Check

```bash
# View your changes
git diff --cached

# Check impact scope
git diff --cached --stat

# Run tests if available
npm test

# Then commit
git commit -v
```

The `-v` flag shows the full diff in your editor while writing the message.

## Branch Management

**Maximum 3 active branches at once.**

If you have more, you're context-switching too much.

**Daily rebase:**
```bash
git fetch origin
git rebase origin/main
```

**Merge fast:**
- Don't let branches live >3 days
- Critical fixes go straight to main
- No "plan" or "evidence" documents needed

## When to Split Commits

Split if:
- Change touches >5 files
- Mixes refactor + feature
- Has independent logical pieces

Example:
```bash
# Instead of one big commit:
git add src/api/
git commit -m "refactor(api): extract workspace client"

git add src/app/
git commit -m "feat(app): use new workspace client"
```

## When NOT to Commit

- Commented-out code (delete it)
- Debug prints (remove them)
- Temp files (add to .gitignore)
- Half-done work (finish the logical unit first)

## Tools Replace Process

Instead of writing docs:
- Use `git log --oneline --graph` to see structure
- Use `git diff main --stat` to see impact
- Use `git blame` to understand history
- Use tests to verify behavior

## Review Your Own Work

Before push:
```bash
# Review all commits on your branch
git log origin/main..HEAD --oneline

# See full diff against main
git diff origin/main

# Check if it makes sense
```

If you can't explain it clearly in the commit message, the code might be unclear too.

## Emergency Hotfix

When business is blocked:

1. Create branch `hotfix/<issue>`
2. Fix the immediate problem (minimal change)
3. Commit with clear message
4. Merge immediately
5. File follow-up refactor as separate task if needed

**Do not let process block critical fixes.**

## Summary

- Write clear commit messages (替代 plan/evidence)
- Keep changes small and focused
- Merge fast, don't let branches rot
- Let tools verify scope (替代手动验证)
- Max 3 active branches (替代复杂冲突管理)

Process serves the work, not the other way around.

Keep this short. This template exists so a reader can understand and reproduce the change months from now — not to satisfy a checker.

Decision records belong in Issues, not here. Link the Issue instead of restating it.

## What changed

<!-- 2-3 sentences. Link the Issue this implements, if any. -->

## Ownership

<!-- DDD owner + phase, if this change belongs to a declared phase. -->

- Owner:
- Phase:

## How to verify

<!-- The commands you actually ran and their real results.
     CI re-runs `npm run verify:local` on every revision, so write the part
     CI does NOT cover: what you checked by hand, and which risk you were
     watching for. -->

- `<command>`: `<actual result>`

## Risk and rollback

<!-- What could break once merged (contract ordering, coordinated deploys,
     migrations), and how to back it out. -->

- Risk:
- Rollback:

---

<!-- 作者自查，合并前可删 -->

- [ ] 分支已同步 main
- [ ] CI 在当前 revision 绿
- [ ] 不含 secret、客户数据或原始 provider 响应
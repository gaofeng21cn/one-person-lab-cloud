# Gateway Key 交互层级实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 按已批准的 UX-03D-B，将 Gateway Key 页面改为结果优先、移动端筛选折叠、低频操作分组，同时保持现有业务 API、权限和敏感值生命周期不变。

**Architecture:** 只在 `KeysPanel` 及现有 Gateway Key CSS 内调整呈现顺序和交互分组。使用原生 `details/summary` 承载移动端“筛选与排序”和每条 Key 的“更多操作”，不引入新的状态管理器或 UI 框架；所有数据和写操作继续走现有 Control Plane adapter。

**Tech Stack:** React 19, TypeScript, existing UI components, native HTML disclosure, existing CSS tokens, Playwright browser tests, Vite.

---

### Task 1: 为 Gateway Key 页面冻结 UX-03D-C 浏览器验收

**Files:**
- Modify: `tests/ui/customer-console-task-experience-browser.test.ts`
- Test: `apps/console-ui/src/components/keys/KeysPanel.tsx`

**Step 1: Write the failing test**

增加 Gateway Key 页面双视口断言：移动端首屏可见第一条 Key 名称/状态和“显示 API 密钥”“使用说明”；“筛选与排序”默认关闭；“更多操作”可见且包含编辑、启停、两种重置、删除；桌面保留表格和所有操作；技术详情默认关闭；无横向溢出和外部请求。

**Step 2: Run test to verify it fails**

Run: `node --test tests/ui/customer-console-task-experience-browser.test.ts`
Expected: FAIL because current mobile layout places filters before the first Key and all seven actions are always同级图标。

---

### Task 2: 重排页面结构并加入原生折叠层

**Files:**
- Modify: `apps/console-ui/src/components/keys/KeysPanel.tsx`

**Step 1: Implement minimal structure**

- 保留标题、Endpoint、刷新和创建入口及所有现有请求调用。
- 将 Key 结果区域作为移动端主内容先于筛选区域渲染。
- 把现有筛选表单包进 `details`，`summary` 文案为 `筛选与排序`，默认关闭；桌面通过 CSS 保持展开显示。
- 将每行操作拆成直接操作（显示/隐藏、使用说明）和 `details` `summary` `更多操作`。
- `更多操作` 内保留编辑、启停、重置配额、重置消费限额、删除，权限禁用状态沿用 `canManage/canDelete`。
- 不修改 DTO、请求函数、`loadKeys`、`reveal`、`mutateKey`、删除确认或 60 秒计时。

**Step 2: Run typecheck**

Run: `npm run typecheck`
Expected: PASS。

---

### Task 3: 调整 Gateway Key 样式和响应式层级

**Files:**
- Modify: `apps/console-ui/src/styles.css`

**Step 1: Implement minimal styles**

- 桌面保持结果表格宽度和可用操作列；直接操作、更多操作、危险操作使用现有 token 做清晰分隔。
- 移动端隐藏桌面表格，先显示 Key 卡片，再显示筛选 disclosure。
- 移动端直接操作保持至少 `44×44px` 触控目标；更多操作内容不产生横向溢出。
- 筛选 disclosure 在移动端默认关闭，在桌面不改变当前单行筛选能力。
- 技术详情保持默认关闭。

**Step 2: Run CSS/source checks**

Run: `git diff --check && npm run lint`
Expected: PASS。

---

### Task 4: 双视口浏览器验证和行为回归

**Files:**
- Modify: `tests/ui/customer-console-task-experience-browser.test.ts` only if semantic selectors need refinement
- Create: `/tmp/one-person-lab-cloud-ux03d-c-2026-09-04/` screenshots outside repository

**Step 1: Run focused browser test**

Run: `node --test tests/ui/customer-console-task-experience-browser.test.ts`
Expected: PASS。

**Step 2: Capture visual evidence**

Use fake-only demo and Playwright at `1280×900` and `390×844`. Capture initial page, mobile filter-open state, mobile more-operations state, and reveal state. Confirm no page errors, Console errors, external requests, or horizontal overflow.

**Step 3: Run repository verification**

Run: `npm run verify:local`
Expected: PASS。

---

### Task 5: 写验证记录并本地提交

**Files:**
- Create: `docs/history/2026-09-04-console-gateway-key-ui-verification.md`
- Modify: `docs/history/README.md`

**Step 1: Record evidence**

记录实现 SHA、双视口截图路径/哈希、focused tests、请求与权威边界、保留的 Key 操作、Reveal 生命周期和未解决问题。明确这是本地 fake-only 验证，不是部署或生产声明。

**Step 2: Commit**

```bash
git add apps/console-ui/src/components/keys/KeysPanel.tsx apps/console-ui/src/styles.css tests/ui/customer-console-task-experience-browser.test.ts docs/history/2026-09-04-console-gateway-key-ui-verification.md docs/history/README.md
git commit -m "feat(console): prioritize gateway key actions"
```

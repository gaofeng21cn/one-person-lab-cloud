# Customer Agent Delivery Materials — stage3-local-agent

Owner: `build 服务（publisher）`，Fabric 负责验证可执行能力
Purpose: `handoff_checklist`
State: `current_checklist`

本文按 [IBD Delivery Materials Checklist](./ibd-delivery-materials-checklist.md) 的五组形状，
交付**一次客户 build 产物**的全部材料。产物身份：

- **交付镜像**：`127.0.0.1:5000/qualification/local-qualification@sha256:73219c72bb67cd7979d4353a5af31f2ff4c11fd8114a4d817240914cbbbcf1b6`
- **平台**：`linux/arm64`
- **机器可读材料**：[`customer-agent.publication.json`](./applications/customer-agent.publication.json)、
  [`customer-agent.revision.json`](./applications/customer-agent.revision.json)、
  [`customer-agent.deployment-descriptor.json`](./applications/customer-agent.deployment-descriptor.json)（产品原始字节）
- **镜像锁**：[`customer-agent-image-lock.md`](./customer-agent-image-lock.md)

交付原则同 checklist 第一段：材料在**内容机器可读、不可变、被校验和绑定**时才算交付 ——
一个能拉取的镜像、一次本地演示或一段口头描述都不算交付。
本材料里的每个 digest 都有服务端回读，且与本仓 JSON 的本地锁**逐字节一致**；
`customer-agent.deployment-descriptor.json` 的摘要由守护测试现算现比。

---

## 1. Images And Runtime Description

Latest consumption: business loop 3, full IBD deployment.

- [x] **Main image**：完整 `127.0.0.1:5000/qualification/local-qualification@sha256:73219c72…`
      引用；平台 `linux/arm64`（**只有** arm64 —— build owner 对着 image config 的
      `os/architecture` 校验过，不是声明出来的）。
- [x] **Every dependent service image**：本 agent **没有**运行期依赖服务镜像。
      构建期吃进去的四份材料（基础镜像 / webui 镜像 / dockerfile 前端 / recipe blob）
      在 `customer-agent.publication.json` 的 `build_inputs` 里单列，**不进交付组件表**。
- [x] **Versioned machine-readable runtime description**：
      `customer-agent.revision.json`（schemaVersion 1）+ 产品原始描述符
      `customer-agent.deployment-descriptor.json`（`opl-deployment-descriptor/v1`）。
      逐项：
      | 项 | 值 |
      | --- | --- |
      | service ports | `http` 3000/TCP（image config `ExposedPorts: 3000/tcp`） |
      | health probes | `GET /` : 3000 |
      | process identity | **未声明** —— 见下 |
      | startup command | Entrypoint `/opt/opl/bin/runtime`，Cmd `serve` |
      | required runtime capabilities | node 22（image config `NODE_VERSION=22.23.2`） |
      | CPU/memory/storage | 交付镜像 84,323,088 bytes；本次下单用 `local-v2-compute`（2 vCPU / 4096 MiB）/ `local-v2-storage`（10 GiB） |
      | persistent mounts | `/data`（mount name `workspace-data`，`concurrentWritersSupported=false`） |
      | scratch mounts | 无 |
      | private dependency connections | 无 |
      | configuration inputs | **无**（publisher 契约未声明任何配置输入） |
- [x] **Configuration vs Secret**：非密配置 **无**；Secret 输入 **无**。
      材料里不含任何密钥值，也不含任何需要被当作密钥对待的字段。

> **process identity 为什么是「未声明」**：publisher 契约的 `applicationRevisionTemplate`
> 没有这个字段，产品渲染出的描述符里也没有；镜像 config 的 `User` 为空。
> 我们**不写** uid/gid —— 没有任何人观测过这个容器真的跑起来，写下数字就是一条没人见证的声明。
> 要填这一格，需要先在 publisher 契约里加字段，再跑一次被观测过的部署。

Verification by Cloud owners: every declared image reference resolves on the
approved registry for every declared platform, and Fabric's declared
capabilities cover every runtime capability requirement.

**本轮实况**：镜像引用在批准的 registry 上解析通过（`GET /v2/qualification/local-qualification/manifests/<digest>`
→ 200，`Docker-Content-Digest` 与声明一致；平台经 image config 复核）。
Fabric 侧的「capabilities 覆盖」**尚未验证** —— 部署腿被宿主内核挡死（见文末「还差哪一环」）。

## 2. Connection Graph And Access

Latest consumption: business loop 3, dependency wiring and application access.

- [x] **Service connection graph**：单组件。交付镜像自己监听 3000/TCP，
      **不调用任何其它组件**，不需要任何私有连接。
- [x] **External services**：**没有**。publisher 契约未声明任何外部依赖。
- [x] **Web entry**：
      | 项 | 值 |
      | --- | --- |
      | root path | `/` |
      | root-URL behavior | 任意路径都返回同一个 JSON 200（agent runtime 按路径回显，不做路由） |
      | session cookie mechanics | **无 cookie**：revision `exposurePolicy=anonymous`，runtime 契约 `applicationAccess.mode=anonymous` |
      | streaming (SSE) | **agent runtime 不实现 SSE**。webui 契约声明 `supportsSse=true`，那是随镜像打包的静态 webui 自身的能力，不是 agent runtime 的能力 —— 两者不可混谈 |
      | self login | **否**：agent 不做自己的登录，也不读任何凭据 |

**判据 D 的可判据标记**：agent runtime 的响应体带 `"service":"stage3-local-agent"`。
平台 fixture 的响应体是 `OPL Workspace READY`（基础镜像的 CMD）。
因此：**看到 `OPL Workspace READY` 就是没在服务这个 agent**；
**看到 `"service":"stage3-local-agent"` 才是**。这两个串互为反面证据，判据 D 不是恒真。

Verification by Cloud owners: the access chain (entry, cookies, streaming)
passes on an isolated environment with a non-OPL application before the IBD
claims production use.

**本轮实况**：**未验证**。访问链没有在隔离环境上跑过一次 —— 部署腿被宿主内核挡死。
本组目前只有**声明**，没有**见证**。

## 3. Data Materials And Restore

Latest consumption: business loop 3, data import; loop 5, restore and rollback.

- [x] **Knowledge-base data set**：本 agent **没有**知识库数据集。唯一的持久位是 `/data`，
      交付时为空。
- [x] **Initial import vs schema-migration restore**：**都不适用**（无数据集）。
      契约侧声明的是包数据语义：`data.schemaVersion=opl-package/v1`，
      `upgrade.mode=compatible`、`upgrade.backupRequired=false`、
      `rollback.safe=true`、`rollback.compatibleSchemaVersions=[opl-package/v1]`。
- [x] **Versioned restore tool + verification tool**：**本 agent 不提供**。
      这是一个诚实的缺口，不是可交付项 —— 没有数据就没有可恢复的东西。
- [x] **Docker volumes / paths 的来源声明**：agent 自带的持久路径只有 `/data`；
      它**不**来自任何宿主 Docker volume，而是 workspace 挂载。

Verification by Cloud owners: a restore into a clean target passes the
publisher's own verification tool, and a real retrieval question returns
answers with reference evidence.

**本轮实况**：不适用（无数据）。若日后 agent 带上数据集，本组必须整体重写。

## 4. Capacity And Platform Requirements

Latest consumption: isolated testing in loop 3; production preflight in loop 5.

- [x] **Per-component and recovery-peak requirements**：
      单组件。交付镜像 84,323,088 bytes（manifest + config + 7 层之和）。
      运行时要求：node 22 运行时在镜像内；`/data` 持久位 ≤ 10 GiB（本次下单的 storage plan）。
      恢复峰值：**未测量**（本 agent 无状态可恢复，仅 `/data`）。
- [x] **Target Workspace 的可用容量、既有绑定、可接受中断窗口**：
      本次下单用的是本地安装的 `local-v2-compute`（2 vCPU / 4096 MiB，`billingMode=local_no_charge`）
      与 `local-v2-storage`（10 GiB）。中断窗口 = `expectedInterruption=none_before_execution`。
      **注意**：本地档位是 `local_no_charge`，容量与中断窗口在 TKE 上才有真值。

Verification by Cloud owners: the sum of components plus recovery peak fits the
target Workspace; a shortfall is returned as an explicit missing list, and
capacity purchase stays a separate action.

**本轮实况**：报价与下单都跑通了（quote 201 / order 202，零计费凭证已出），
即「容量能不能装下」这一层**没有**被拒；真正挡住的是 fabric 的存储预检（第 5 组的宿主内核前提）。

## 5. Registry, Secret Interfaces And Instance Bindings

Latest consumption: generic interfaces with non-sensitive fixtures in loop 3;
production values only at the Instance owner boundary in loop 5.

- [x] **Registry connection facts**：
      | 项 | 值 |
      | --- | --- |
      | registry | `127.0.0.1:5000`（本地资格安装的明文 HTTP registry） |
      | repositories | `qualification/local-qualification`（交付）、`opl-qualification-workspace/*`（build 输入） |
      | 访问授权 | 安装事实里的 `OPL_BUILD_REGISTRY_TOKEN`（安装器生成，不落进本仓） |
      | 部署描述符材料 | `build-artifact://artifact_build_b3b08d7f…/descriptor@sha256:37f8c298…`（owner 内部对象引用，非 registry 路径） |
- [x] **Secret inputs**：**没有 Secret 输入**。因此没有 consumer / format / rotation 需要声明。
- [x] **DNS / TLS / origin / provider-policy**：
      本次交付是本地档位：无 DNS、无 TLS 终止、无 origin 策略。
      换 provider（TKE）时这些位由 Instance owner 在自己的受保护流程里绑定，
      生产 Secret 值不经过 Cloud 代码或日志。

Verification by Cloud owners: non-sensitive fixtures pass through the same
interfaces in isolated tests; production Secret values are consumed only by the
Instance owner's protected workflow and never transit Cloud code or logs.

**本轮实况**：接口事实齐了；「非敏感 fixture 走同一套接口」在**部署腿**上还没被走过一次。

---

## 还差哪一环（诚实的账）

五组里，**组 1 的镜像引用**与**组 4 的报价/下单**已有服务端回读；
**组 2 的访问链**与**组 5 的接口走通**还缺一次**真的部署**。

缺的那一环不是产品逻辑，是**宿主内核能力**：fabric 的 Local-Docker 存储配额预检要求
存储根是一个带 `prjquota` 的独立 ext4/xfs 挂载，且内核要能跑 QFMT_V2 的 project quota；
Docker Desktop 的 `6.12.76-linuxkit` 内核 `# CONFIG_QFMT_V2 is not set`、无 `CONFIG_PROJECT_QUOTA`，
全机也没有任何带 quota 选项的挂载。CI 的 ubuntu runner 会自己造出这块存储根
（`mkfs.ext4 -O project,quota -E quotatype=prjquota` + `mount -o loop,prjquota`）。

补上那一环之后，判据 D 的见证方式是固定的：**部署到隔离环境 → 打开 URL →
响应体必须含 `"service":"stage3-local-agent"`，且不得含 `OPL Workspace READY`**。

## Consumption Order

Loop 1 and loop 2 do not wait for any item above. Loop 3 keeps the
customer-agent-specific acceptance open until groups 1–4 pass their
verification; a non-OPL application proves the generic path meanwhile.
Group 5 separates fixture development from production values so nothing here
becomes a total gate on Cloud work.

本材料的**当前消费点**：组 1（镜像与运行时描述）可以被下游直接消费；
组 2 与组 5 的**见证**要等部署腿在带 project quota 的 Linux 上跑通；
组 3、组 4 对本 agent 基本不适用。

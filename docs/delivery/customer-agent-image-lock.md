# Customer Agent Image Lock — build_b3b08d7f3a739eff9525481b243a5165

本文锁定**一次客户 build 产物**的服务端身份。产物由 build 服务推送（buildx `--push`），
发布后由 build owner 以 `Docker-Content-Digest` 读回核验，并由本仓的守护测试
(`tests/delivery/customer-agent-descriptor.test.ts`) 校验下表与本仓
`docs/delivery/applications/` 下三份 JSON 的**本地锁逐字节一致**。

**交付组件只有一个。** 其余四行是这次 build **吃进去的输入**，不是交付物：它们不作为服务运行，
也不许被当成交付内容（详见 `customer-agent.publication.json` 的 `build_inputs`）。

| 组件 | 仓库引用 | 服务端 digest | 本地锁一致 |
| --- | --- | --- | --- |
| agent(主应用) | `127.0.0.1:5000/qualification/local-qualification` | `sha256:73219c72bb67cd7979d4353a5af31f2ff4c11fd8114a4d817240914cbbbcf1b6` | ✅ |
| build 输入 · 基础镜像 | `127.0.0.1:5000/opl-qualification-workspace` | `sha256:ae7b48c14ccb26e108a69bbad0ff88f2adae45cc4dfcf220d85213ed02f6b7b3` | ✅ |
| build 输入 · webui 镜像 | `127.0.0.1:5000/opl-qualification-workspace/webui` | `sha256:c2b819f4293f36212656abab0f918b9ab9513ec3e52085a6717874d450db23c2` | ✅ |
| build 输入 · dockerfile 前端 | `127.0.0.1:5000/opl-qualification-workspace/dockerfile` | `sha256:e291cde6b1429712d3951bca77be92338719212df6654812f1a55874a481742d` | ✅ |
| build 输入 · recipe blob | `127.0.0.1:5000/opl-qualification-workspace/recipe` | `sha256:4e60cda483cc0bc0bcb59818bd7c42ed7574bef8ee1db7ea9ec0cf6745c96d48` | ✅ |

**身份不许混用**：本发布方只产出**单平台 OCI image manifest**，从不产出 OCI index
（`services/build/internal/build/runner.go` 的 `ReadManifest` 只请求 manifest 两种 media type，
遇到 index 直接报 `single-platform image manifest required`）。所以上表的 digest **就是**平台 manifest
自身的身份；**没有** index digest，**也没有** platform child manifest digest 可供比对。
把 index digest 与 platform child manifest digest 混着写、或用其中之一冒充另一个，都是错的。

**交付产物不是历史产物**：`sha256:ae7b48c1…` 是基础镜像（本次 build 的 FROM 输入），
它的 CMD 会吐出平台 fixture 正文 `OPL Workspace READY`。交付镜像的 Entrypoint 是
`/opt/opl/bin/runtime`（客户 Package 自带的 runtime）。**看到 `OPL Workspace READY`
就说明服务的是平台 fixture，不是这个 agent** —— 这是判据 D 的反面证据。

## 来源绑定

- 交付包：build job `build_b3b08d7f3a739eff9525481b243a5165`，
  artifact `artifact_build_b3b08d7f3a739eff9525481b243a5165`，
  operation `op_d105f70381317ad5288b515fe74321b9`
- 冻结清单：`docs/delivery/applications/customer-agent.deployment-descriptor.json`
  （产品 build 服务写出的 `deployment_descriptor` 原始字节，4644 bytes），
  其摘要 `sha256:37f8c298f7513f24bda6da04d76cc183ffb09196181c8dcf9c5da1dc2a385b6f`
  与产品自己记的 `deployment_descriptor_digest` **逐字节一致**
- 客户 Package：`pv_79fe03919bafd55de576274415579445`，
  sha256 `sha256:72eb3ce7d813aed92059072673a5b61d7581424e8f9499c2fed351d22a6c7ad2`，1054 bytes
- publisher 契约：runtime `runtime_c365af1b4617584a9d06ee99ca118003`
  （契约摘要 `sha256:520dde71…`）、webui `webui_8969beb864a4392a39b22222b9cf2f9d`
  （契约摘要 `sha256:1ed81482…`）、build policy `policy_3c943b37a6660b0d655e31c350f79f45`
- 原始上游：基础镜像是本机现建的**替身**（见下 Fixed Local Reference）；
  webui 镜像是公开发布的 `one-person-lab-webui`（linux/arm64）

## 凭据要求

- 本地 registry 为**明文 HTTP**，访问凭据是安装事实里的 `OPL_BUILD_REGISTRY_TOKEN`
  （安装器生成，不落进本仓任何文件）
- 节点拉取：local-docker provider 走本机 docker socket，**不需要** pull secret；
  换到 TKE 时该位要绑成安装级 `OPL_IMAGE_PULL_SECRET_NAME`（指向同一凭据的 K8s pull secret）

## Fixed Local Reference

这次交付的运行时引用是**本地资格安装** `opl-native-stage2-local-1`
（compose 项目），不是后来的任何一次本机重建：

- 云镜像：真实已发布 Candidate `ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:6cb322c6892a609b4819ac7b2718906adb8b5bd7cdf510bc7518d8eaf946e85a`
  （source sha `e25ab3ee`）
- 平台镜像：基础镜像是本机用 `deploy/portable/qualification-workspace.Dockerfile` 现建的**替身**
  （零 RUN、零网络），digest `sha256:ae7b48c1…`

**边界**：这是一个**资格产物**，不是生产 OPL App 交付。它能证明「build 产出的镜像身份是可锁的、
可回读、可复现的」；它**不能**证明真实 OPL App 镜像能被同样地构建与服务。
把基础镜像换成上游签的、指向真实 App 镜像的同一 digest 之后，本文的基础镜像行必须一起换掉。

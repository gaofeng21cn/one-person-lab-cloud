# IBD Cloud Image Lock — 20260909-verified

七个镜像全部来自 IBD 交付包 `ibd-assistant-full-20260909` 的 `images/*.oci.tar`
与 `bundle-manifest.json` 冻结的 `image_id`。推送方式:OCI 布局逐 blob 直连上传
(绕开代理链),manifest PUT 后以 `Docker-Content-Digest` 读回核验。全部七项
服务端 digest 与本地 `images.env` 锁**逐字节一致**;推送过程未重建、未改 tag 内容。

| 组件 | TCR 引用(tag `20260909-verified`) | 服务端 digest | 本地锁一致 |
| --- | --- | --- | --- |
| agent(主应用) | `uswccr.ccs.tencentyun.com/oplcloud/chaokang_agent_ibd` | `sha256:2fcfa6cd799ada43f6977621da9d7e2595a0b608c9207b9eaf06dd150f6fcf64` | ✅ |
| ragflow | `uswccr.ccs.tencentyun.com/oplcloud/ibd-ragflow` | `sha256:1e90b41aba4e2007de1ed1ab0e859c821e8bda2722fb94aa0cddf8a5814ab9ec` | ✅ |
| tei(向量模型) | `uswccr.ccs.tencentyun.com/oplcloud/ibd-tei` | `sha256:ad4a00f5af757f7f323bdeb9e873313ed71225cf57d94658cbc9c6c17dc67d85` | ✅ |
| es01(Elasticsearch) | `uswccr.ccs.tencentyun.com/oplcloud/ibd-es01` | `sha256:0e47a784b266b7f30d8071a25f774b10a38f3bb9bb41e23a2767ba464aafcb21` | ✅ |
| mysql | `uswccr.ccs.tencentyun.com/oplcloud/ibd-mysql` | `sha256:ed04aca46b6fe0bdc192becc17d358b46eeb82762b81ee65b80feff3d0873e9d` | ✅ |
| minio(silo) | `uswccr.ccs.tencentyun.com/oplcloud/ibd-minio-silo` | `sha256:da1284931914c3de5fc8e8ad0c43b88e4eb930d20064b36867daba4dfd546a00` | ✅ |
| valkey(redis) | `uswccr.ccs.tencentyun.com/oplcloud/ibd-valkey` | `sha256:f740fb12fb9dec890f4785ebd04ba184ab9bc556cc33063a01726a9180cc697c` | ✅ |

alpine 恢复工具(3.7MB,官方镜像)不入库,部署机直接 `docker pull alpine`。

## 来源绑定

- 交付包:`~/Desktop/ibd2/output/ibd-assistant-full-20260909`
- 冻结清单:`bundle-manifest.json`(各镜像 `image_id` = 上表 digest)
- 原始上游:ragflow `swr.cn-north-4.myhuaweicloud.com/infiniflow/ragflow:v0.27.1`;
  tei `infiniflow/text-embeddings-inference:cpu-1.8`(daocloud 镜像源);
  valkey `public.ecr.aws/valkey/valkey:8.1.10`;minio `pgsty/silo:RELEASE.2026-08-06T00-00-00Z`;
  es/mysql 为 IBD 作者构建(`ibd-es:amd64-20260909`、`ibd-mysql:amd64-20260909`)

## 凭据要求(已实测:命名空间为私有)

- 浏览/解析:`OPL_WORKSPACE_REGISTRY_USERNAME=100047070895` + 对应密码
- 节点拉取:安装级 `OPL_IMAGE_PULL_SECRET_NAME` 指向含同一凭据的 K8s pull secret

## Fixed Local Reference

The runtime reference is the original `ibd-assistant-full-20260909` bundle and
Compose project `ibd-full-20260909-03`, not a later Docker Desktop experiment.
The bundle manifest SHA-256 is
`0e0b15dd132223cdce3c193b76303f5cc9d42881d4d54448f89e4be467bcc05b`.
Its own `deploy.py --phase verify` checked all 74 files and eight image archives
on 2026-09-15 without creating containers or changing data. The eighth archive is
the original restoration helper, not another running application component.

Preserve the four independent knowledge-data bindings, restored embedding model,
original component settings and authenticated health checks. The main image's
three knowledge-file environment paths already point at files baked into that
image; they are configuration, not a new file-import feature. The main `/state`
is an owned executable tmpfs, not a persistent host directory. Original parsing
and indexing are not repeated; MinerU and Temporal are not startup dependencies.

The application root OCI index is `2fcfa6cd…`; its `linux/amd64` child manifest is
`d04777dd…`. These are distinct identities in the same archived index, not a
replacement image. Qualification must retain both layers instead of comparing
a platform-selected manifest against an index as though they were identical.

Original retained artifacts prove healthy startup, restored 20-document/
2,101-chunk knowledge data, and one successful streamed answer with citations.
The outer image-bound acceptance report still records an artifact-identity
failure; this is a usable runtime reference, not a completed Cloud or clinical
qualification. A new run must bind its actual source, image, input and result
identities. Real model configuration is an external read-only input and is not
present in the bundle or product source.

Current reference-lock and non-secret derived deployment material are retained
under the canonical checkout's `output/ibd-baseline-20260915/`; its transient
`private/` test inputs and case-sensitive scratch disk image are not retained.
They are qualification inputs, not a second product registry. A new
original-image question with the user-selected `glm-5.3-flash` passed
HTTP/container receipt, SSE and citation checks; see `docs/status.md` for its
exact run evidence.
This does not prove product data restore, Console/Control Plane replacement,
or Tencent Instance adoption.

The non-secret reference projection keeps Elasticsearch's original dotted
environment settings, image and authentication. It does not replace the baked
configuration with a generated approximation. Redis keeps its original image
entrypoint; its password-bearing arguments are represented by a readonly Secret
configuration file with the same password, memory ceiling and eviction policy.
An isolated original-image check verified this transport without mounting old
knowledge volumes or exposing a host port. No credential value is recorded here.

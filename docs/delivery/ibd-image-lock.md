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

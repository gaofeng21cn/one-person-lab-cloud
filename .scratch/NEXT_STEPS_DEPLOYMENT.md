# 🚀 部署到生产环境 - 下一步操作

## ✅ 已完成的准备工作

### 1. 代码回退到8月30日版本
- **目标commit**: `3934b6e73b104298a23ba03f6c539fe3216264e3`
- **分支**: `deploy-test-aug30-for-medopl` 
- **标签**: `v0.1.8-testcloud-aug30`
- **状态**: ✅ 已推送到GitHub

### 2. Instance配置更新
- **仓库**: opl-instance-medopl
- **PR #460**: ✅ 已合并到main分支
- **配置文件**: `instance/medopl.json` 已更新指向8月30日版本
- **验证**: ✅ 本地测试全部通过

### 3. 本地验证
- ✅ docker-compose运行正常
- ✅ Fabric和Ledger服务健康
- ✅ Control Plane等待Sub2API（预期行为）
- ✅ 所有单元测试通过

---

## 🎯 当前状态

**opl-instance-medopl main分支已经配置为部署8月30日版本**

配置内容：
```json
{
  "deployment": {
    "mode": "managed_tke",
    "status": "deployment_test_aug30",
    "productSha": "3934b6e73b104298a23ba03f6c539fe3216264e3",
    "productReleaseTag": "v0.1.8-testcloud-aug30",
    "authority": "opl-instance-medopl-main-production-environment"
  }
}
```

---

## 🔧 需要的额外步骤

### 步骤1：构建Cloud镜像

8月30日版本的镜像需要重新构建并推送到registry。有两个选项：

#### 选项A：使用GitHub Actions构建
在one-person-lab-cloud仓库中触发构建workflow：
```bash
cd /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud
gh workflow run <build-workflow-name> --ref deploy-test-aug30-for-medopl
```

#### 选项B：本地构建并推送
```bash
cd /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud
git checkout 3934b6e73b104298a23ba03f6c539fe3216264e3

# 构建镜像
docker build -t ghcr.io/gaofeng21cn/one-person-lab-cloud:v0.1.8-testcloud-aug30 .

# 推送到GitHub Container Registry
docker push ghcr.io/gaofeng21cn/one-person-lab-cloud:v0.1.8-testcloud-aug30
```

### 步骤2：生成Candidate Receipt

部署workflow需要 `candidate_receipt_b64` 参数。这通常由Cloud的构建系统生成。

查看构建流程：
```bash
cd /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud
ls -la tools/ | grep candidate
```

可能需要运行类似：
```bash
node tools/cloud-candidate-receipt.ts create \
  --product-sha 3934b6e73b104298a23ba03f6c539fe3216264e3 \
  --cloud-image ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:<digest> \
  --workspace-image <tcr-workspace-image> \
  --output candidate-receipt.json
```

### 步骤3：触发Deploy Workflow

使用生成的candidate receipt触发部署：

```bash
cd /Users/huangrende/Documents/ChatGPT/opl-instance-medopl

# 方式1：通过GitHub UI
# https://github.com/gaofeng21cn/opl-instance-medopl/actions/workflows/deploy.yml
# 点击 "Run workflow"
# 选择 branch: main
# deployment_mode: release
# candidate_receipt_b64: <base64编码的receipt>

# 方式2：通过gh CLI
RECEIPT_B64=$(cat candidate-receipt.json | base64)
gh workflow run deploy.yml \
  --ref main \
  -f deployment_mode=release \
  -f candidate_receipt_b64="$RECEIPT_B64"
```

---

## 📋 完整部署检查清单

- [ ] **步骤1**: 构建8月30日版本的Cloud镜像
- [ ] **步骤2**: 生成Candidate Receipt
- [ ] **步骤3**: 触发Deploy workflow
- [ ] **步骤4**: 监控部署过程
- [ ] **步骤5**: 部署成功后运行Deployment Verification
- [ ] **步骤6**: 端到端测试

---

## 🧪 端到端测试计划

部署成功后：

1. **访问Console**
   - URL: https://cloud.medopl.com
   - 检查健康状态: https://cloud.medopl.com/api/healthz

2. **用户登录测试**
   - 账号: testcloud4@medopl.com
   - 账号: testcloud3@medopl.com

3. **创建Workspace**
   - 登录后点击"创建Workspace"
   - 填写名称，例如：`test-workspace-aug30-recovery-001`
   - 提交并观察创建过程

4. **验证成功标准**
   - [ ] Workspace创建请求提交成功
   - [ ] 状态从"创建中"变为"运行中"
   - [ ] 能够访问Workspace应用
   - [ ] 应用功能正常
   - [ ] Ledger有对应的receipt记录

---

## 🔍 替代方案：直接使用v0.1.7

如果构建8月30日版本遇到困难，可以考虑：

**使用已知可工作的v0.1.7版本** (commit a59bde68)：
- 这个版本在8月15日成功部署
- 镜像已经存在：`ghcr.io/gaofeng21cn/one-person-lab-cloud:v0.1.7`
- 有完整的deployment receipt

修改配置指向v0.1.7：
```bash
cd /Users/huangrende/Documents/ChatGPT/opl-instance-medopl
# 编辑 instance/medopl.json
# 将 productSha 改为 a59bde68397528186a5220f73195fa1f3eda311b
# 将 productReleaseTag 改为 v0.1.7
```

---

## 📞 需要确认的信息

1. **Sub2API服务地址**
   - 生产环境的Sub2API服务是否已经运行？
   - 地址是什么？需要在环境变量中配置

2. **TKE集群访问**
   - 确认有kubectl访问权限
   - 确认KUBECONFIG已配置在GitHub Secrets中

3. **Registry访问**
   - GHCR (GitHub Container Registry) 推送权限
   - TCR (Tencent Container Registry) 访问权限

---

## 💡 建议

**最快速的验证路径**：

1. 先使用v0.1.7（已有镜像）进行一次完整的部署测试
2. 验证端到端流程是否工作
3. 如果成功，说明基础设施正常
4. 然后再决定是否需要部署8月30日的版本

**问题诊断顺序**：

1. 确保Sub2API服务可用
2. 确保数据库连接正常
3. 确保TKE集群健康
4. 检查Fabric provider配置
5. 最后测试workspace创建

---

**下一步操作建议**：告诉我你想：

A. 构建8月30日版本的新镜像并部署
B. 使用现成的v0.1.7版本快速验证
C. 检查Sub2API和基础设施状态
D. 其他方案

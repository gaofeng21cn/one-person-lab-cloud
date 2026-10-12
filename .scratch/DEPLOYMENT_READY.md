# ✅ 部署就绪 - v0.1.7版本

## 🎉 配置已完成

### ✅ 所有准备工作完成

1. **PR #461已合并到main** ✅
2. **配置已更新为v0.1.7** ✅
3. **所有验证测试通过** ✅
4. **镜像已存在于registry** ✅

---

## 📋 当前配置

```json
{
  "productSha": "a59bde68397528186a5220f73195fa1f3eda311b",
  "productReleaseTag": "v0.1.7",
  "imageDigestRef": "sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f",
  "authority": "opl-instance-medopl-main-production-environment"
}
```

**镜像地址**: `ghcr.io/gaofeng21cn/one-person-lab-cloud@sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f`

---

## 🚀 下一步：触发部署

### 方式1：通过GitHub UI（推荐）

1. 访问: https://github.com/gaofeng21cn/opl-instance-medopl/actions/workflows/deploy.yml
2. 点击 **"Run workflow"** 按钮
3. 确认分支选择为 **main**
4. 输入参数：
   - **deployment_mode**: 选择 `release`
   - **candidate_receipt_b64**: 需要v0.1.7的candidate receipt（base64编码）

### 方式2：通过gh CLI

```bash
cd /Users/huangrende/Documents/ChatGPT/opl-instance-medopl

# 需要先获取v0.1.7的candidate receipt
# 然后执行：
RECEIPT_B64="<base64_encoded_receipt>"

gh workflow run deploy.yml \
  --ref main \
  -f deployment_mode=release \
  -f candidate_receipt_b64="$RECEIPT_B64"
```

---

## 📦 关于Candidate Receipt

Deploy workflow需要一个`candidate_receipt_b64`参数。这是Cloud构建系统生成的不可变的产品标识。

### 选项A：使用历史receipt（如果存在）

查找v0.1.7的candidate receipt：
```bash
cd /Users/huangrende/Documents/ChatGPT/one-person-lab-cloud
# 查看构建记录或artifacts
```

### 选项B：从现有部署推断

由于v0.1.7在2026年8月15日成功部署过，可能在以下位置有receipt：
- GitHub Actions artifacts
- 部署记录中
- Instance仓库的receipts目录

### 选项C：查看最近成功的部署

```bash
cd /Users/huangrende/Documents/ChatGPT/opl-instance-medopl
gh run list --workflow=deploy.yml --limit 10 | grep success
```

查看成功运行的输入参数可以获得receipt格式。

---

## 🧪 部署后的验证步骤

### 1. 监控部署
```bash
# 获取运行ID后
gh run watch <run-id>
```

### 2. 验证服务健康
```bash
# 等待部署完成后
curl https://cloud.medopl.com/api/healthz
```

### 3. 登录测试
- URL: https://cloud.medopl.com
- 账号: testcloud4@medopl.com 或 testcloud3@medopl.com

### 4. 创建Workspace
1. 登录控制台
2. 点击"创建Workspace"
3. 输入名称：`test-v017-validation-001`
4. 提交并观察创建过程

### 5. 验证成功标准
- [ ] Workspace创建请求提交成功
- [ ] 状态变为"运行中"或"就绪"
- [ ] 能访问Workspace应用
- [ ] 应用功能正常
- [ ] Ledger有receipt记录

---

## 🔍 如果没有Candidate Receipt

如果找不到v0.1.7的candidate receipt，有以下选项：

### 选项1：查看Bootstrap或First Install Receipt

```bash
cat /Users/huangrende/Documents/ChatGPT/opl-instance-medopl/receipts/2026-08-15-first-tke-deployment.json
```

这个receipt可能包含部署时使用的镜像信息。

### 选项2：直接查看成功的Deploy运行

```bash
gh run view 37976877082 --log | grep -i "candidate" -A 5 -B 5
```

### 选项3：请gaofeng21cn重新构建

如果必须有新的candidate receipt，需要gaofeng21cn账号执行：
```bash
gh workflow run build-opl-cloud-candidate.yml \
  --ref main \
  -f product_sha=a59bde68397528186a5220f73195fa1f3eda311b
```

---

## 📊 部署历史参考

**上次成功部署v0.1.7**:
- 日期: 2026-08-15
- Commit: a59bde68397528186a5220f73195fa1f3eda311b
- 镜像digest: sha256:e64504731f8b61c0864cf59faa647a1150e8a2a5eada34b26faf3a5487d28e8f
- Receipt: receipts/2026-08-15-first-tke-deployment.json

**最近成功部署**:
```bash
# 2026-10-09
gh run view 37976877082
gh run view 37973585718
```

---

## 💡 推荐的行动方案

1. **查找历史receipt** - 检查最近成功的deploy运行
2. **如果找到receipt** - 直接触发部署
3. **如果找不到** - 请gaofeng21cn重新构建candidate
4. **部署完成后** - 立即进行testcloud账号验证

---

## 📞 当前状态总结

- ✅ **代码**: v0.1.7 (a59bde68)
- ✅ **配置**: 已合并到main
- ✅ **镜像**: 存在于registry
- ⏸️ **部署**: 等待candidate receipt
- ⏸️ **测试**: 等待部署完成

**下一步操作**: 获取或生成v0.1.7的candidate receipt，然后触发Deploy workflow。

---

需要我帮你：
A. 查找历史receipt
B. 准备构建命令给gaofeng21cn
C. 其他协助？

# 部署到 opl-instance-medopl 的步骤

## 版本信息
- **Commit**: 3934b6e73b104298a23ba03f6c539fe3216264e3
- **日期**: 2026-08-30
- **描述**: feat(fabric): reconcile workspace node image gc
- **目的**: 这是testcloud3/testcloud4成功开通时期的版本

---

## 部署步骤

### 1. 推送这个版本到远程

```bash
# 创建一个部署分支
git checkout -b deploy-test-2024-08-30 3934b6e73b104298a23ba03f6c539fe3216264e3

# 推送到远程
git push origin deploy-test-2024-08-30

# 或者创建标签
git tag -a v0.1.8-test-20240830 -m "Test deployment for testcloud3/testcloud4" 3934b6e73b104298a23ba03f6c539fe3216264e3
git push origin v0.1.8-test-20240830
```

### 2. 在 opl-instance-medopl 中配置

需要去 opl-instance-medopl 仓库中：

```bash
# 假设在另一个目录
cd ../opl-instance-medopl

# 更新要部署的版本
# 修改配置文件，指向新的commit或tag
# 例如在 .github/workflows/ 或配置文件中更新：
# OPL_CLOUD_SHA=3934b6e73b104298a23ba03f6c539fe3216264e3
# 或
# OPL_CLOUD_TAG=v0.1.8-test-20240830
```

### 3. 触发部署

根据 opl-instance-medopl 的部署流程：
- 如果是自动部署：推送更改会触发
- 如果是手动部署：在GitHub Actions中手动触发
- 如果是命令行部署：运行相应的部署脚本

---

## 验证步骤

部署完成后，测试testcloud4@medopl.com账号：

### 1. 登录
```
URL: https://your-instance.medopl.com
Email: testcloud4@medopl.com
Password: [你知道的测试密码]
```

### 2. 创建Workspace
- 点击"创建Workspace"
- 填写名称和配置
- 提交并观察

### 3. 验证成功标准
- [ ] Workspace创建成功
- [ ] 状态变为"运行中"或"就绪"
- [ ] 能访问Workspace的App
- [ ] 能进行基本操作（发送消息等）

---

## 当前one-person-lab-cloud仓库状态

```bash
# 查看当前状态
git status

# 如果需要，可以推送这个测试分支
git checkout -b test-deploy-aug30
git push origin test-deploy-aug30
```

---

## 如果我来帮你部署

请告诉我：
1. **opl-instance-medopl 的仓库地址** 或者你想让我直接操作吗？
2. **部署配置在哪里**？在哪个文件中指定cloud的版本？
3. **部署方式**：GitHub Actions、脚本、还是手动？

我可以帮你：
- ✅ 推送这个版本的分支/标签
- ✅ 生成部署配置
- ✅ 准备部署命令
- ✅ 创建部署检查清单

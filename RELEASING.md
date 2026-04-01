# 发布说明

## 发布前检查

- [ ] 工作区状态已确认，`git status` 中没有未处理但要保留的改动
- [ ] Go 测试通过：`cd mcp-server-go && go test ./...`
- [ ] Go 构建通过：`cd mcp-server-go && go build ./...`
- [ ] 版本号符合 [SemVer](https://semver.org/)

## 版本规则

- **MAJOR**：不兼容变更
- **MINOR**：向后兼容的新功能
- **PATCH**：向后兼容的修复

## 发布流程

### 1. 打 tag 并推送

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

推送 `v*` tag 后会自动触发发布工作流。

### 2. 检查 Actions

成功后会得到：

- GitHub Release
- 三个平台压缩包
- `.mcpb` bundle
- `server.json`
- `SHA256SUMS`

## 当前发布包内容

- 会打包主文档与手册
- 不再打包 `opencode-agents`
- 不再打包 `docs/wiki/`

## 如果工作流失败

1. 不要删 tag，也不要重打同名 tag
2. 本地修复问题
3. 升一个 patch 版本重新发

```bash
git add .
git commit -m "fix: <description>"
git tag v1.2.4
git push origin main v1.2.4
```

## 校验

```bash
sha256sum -c SHA256SUMS
```

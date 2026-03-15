# MCP Registry 发布要点备忘

> **说明**: 本文档整理自官方 MCP Registry 发布流程，用于快速查阅关键步骤与要求。
>
> **注意**: `modelcontextprotocol.info` 可能是镜像/整理站点，以下仅引用 `modelcontextprotocol.io` 与官方 GitHub 作为权威来源。

---

## 1. MCP Registry 是什么 / 不是什么

**官方定义**: MCP Registry 是 MCP 服务器的官方集中式元数据仓库（metadata registry），由 Anthropic、GitHub、PulseMCP、Microsoft 等主要生态贡献者支持。

**关键特征**:
- ✅ **只存元数据**（server.json），**不存放 artifact**（代码/二进制）
- ✅ **只支持 public servers**（公开可访问的包或远程服务）
- ❌ **不支持 private servers**（私有网络、私有包仓库）
- ✅ 提供 REST API 供下游聚合器/市场发现服务器

**参考**: [MCP Registry About](https://modelcontextprotocol.io/registry/about)

---

## 2. server.json 的核心地位

**定义**: 所有服务器元数据均以标准化 `server.json` 格式存储，Schema 定义见 [server.schema.json](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/server.schema.json)。

**关键字段**:
- `name`: 服务器唯一标识（反向 DNS 格式，如 `io.github.username/server-name`）
- `version`: 语义化版本（必须唯一，一旦发布不可变更）
- `packages`: 包含 `registryType`、`identifier`、`transport` 等
- `repository`: 代码仓库信息（`url`、`source`）

**不可变性**: 已发布的 `version` 及其元数据**无法修改**，只能发布新版本。

**参考**: [Quickstart - server.json 创建](https://modelcontextprotocol.io/registry/quickstart#step-4-create-serverjson)

---

## 3. 认证与命名空间

**核心原则**: 认证方式决定命名空间格式。

| 认证方式 | 名称格式 | 示例 |
|---------|---------|------|
| GitHub | `io.github.{username}/*` 或 `io.github.{orgname}/*` | `io.github.alice/weather-server` |
| Domain (DNS/HTTP) | `com.example.*/*` (反向 DNS) | `io.modelcontextprotocol/everything` |

**认证类型**:
1. **GitHub OAuth**: 通过 `mcp-publisher login github`，访问 https://github.com/login/device 输入设备码
2. **DNS Authentication**: 在域名添加 TXT 记录 (`v=MCPv1; k=ed25519; p=...`)
3. **HTTP Authentication**: 在域名托管 `/.well-known/mcp-registry-auth` 文件

**参考**: [Authentication Methods](https://modelcontextprotocol.io/registry/authentication)

---

## 4. 支持的 Package Types + Ownership Verification

### 4.1 npm
- **registryType**: `"npm"`
- **验证方式**: 在 `package.json` 中添加 `mcpName` 字段
  ```json
  {
    "name": "@username/my-server",
    "version": "1.0.0",
    "mcpName": "io.github.username/my-server"
  }
  ```
- **支持范围**: 仅支持官方 npm public registry (`https://registry.npmjs.org`)

### 4.2 PyPI
- **registryType**: `"pypi"`
- **验证方式**: 在 README 中添加隐藏注释
  ```markdown
  <!-- mcp-name: io.github.username/my-server -->
  ```
- **支持范围**: 仅支持官方 PyPI (`https://pypi.org`)

### 4.3 NuGet
- **registryType**: `"nuget"`
- **验证方式**: 与 PyPI 相同，在 README 中添加 `mcp-name:` 注释

### 4.4 Docker/OCI Images
- **registryType**: `"oci"`
- **identifier 格式**: `registry/namespace/repository:tag` (如 `docker.io/user/app:1.0.0`)
- **验证方式**: 在 Dockerfile 添加 LABEL
  ```dockerfile
  LABEL io.modelcontextprotocol.server.name="io.github.username/my-server"
  ```
- **支持范围**: Docker Hub、GitHub Container Registry、Google Artifact Registry、Azure Container Registry、Microsoft Container Registry

### 4.5 MCPB Packages
- **registryType**: `"mcpb"`
- **identifier**: GitHub/GitLab Releases 的 `.mcpb` 文件 URL
- **验证要求**:
  - URL 必须包含字符串 `"mcp"`
  - 必须提供 `fileSha256` 字段（SHA-256 哈希值）
  ```bash
  openssl dgst -sha256 my-server.mcpb
  ```

**参考**: [Package Types Documentation](https://modelcontextprotocol.io/registry/package-types)

---

## 5. 发布工具链 mcp-publisher

**安装** (macOS/Linux):
```bash
curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar xz mcp-publisher && sudo mv mcp-publisher /usr/local/bin/
```

**核心命令**:
```bash
# 1. 初始化 server.json 模板（从项目自动提取信息）
mcp-publisher init

# 2. 认证（三选一）
mcp-publisher login github          # GitHub OAuth
mcp-publisher login github-oidc     # GitHub Actions OIDC
mcp-publisher login dns --domain example.com --private-key "..."
mcp-publisher login http --domain example.com --private-key "..."

# 3. 发布到 Registry
mcp-publisher publish
```

**参考**: [Quickstart - Install mcp-publisher](https://modelcontextprotocol.io/registry/quickstart#step-3-install-mcp-publisher)

---

## 6. 自动化：GitHub Actions + OIDC

### 关键要点
- **推荐使用 OIDC 认证**，无需管理 PAT token
- **必需权限**: `id-token: write`（用于 OIDC）+ `contents: read`

### Workflow 示例（OIDC 认证）
```yaml
name: Publish to MCP Registry
on:
  push:
    tags: ["v*"]  # 触发器：版本标签

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # OIDC 认证必需
      contents: read

    steps:
      - name: Checkout code
        uses: actions/checkout@v5

      # ... 构建/测试/发布底层包的步骤 ...

      - name: Install mcp-publisher
        run: |
          curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar xz mcp-publisher

      - name: Authenticate to MCP Registry
        run: ./mcp-publisher login github-oidc

      - name: Publish server to MCP Registry
        run: ./mcp-publisher publish
```

### Secrets 配置
- **OIDC**: 无需额外 secret（推荐）
- **PAT**: 添加 `MCP_GITHUB_TOKEN` secret（需要 `read:org` 和 `read:user` scopes）
- **DNS**: 添加 `MCP_PRIVATE_KEY` secret（Ed25519 私钥）

**参考**: [GitHub Actions Automation](https://modelcontextprotocol.io/registry/github-actions)

---

## 7. 验证发布结果

### 7.1 Registry API 查询
```bash
# 搜索特定服务器
curl "https://registry.modelcontextprotocol.io/v0.1/servers?search=io.github.username/server-name"

# 获取服务器详情（通过 ID）
curl "https://registry.modelcontextprotocol.io/v0.1/servers/{server-id}"
```

### 7.2 常见错误排查
- **"Registry validation failed for package"**: 检查包中是否包含必需的验证信息（如 `mcpName`）
- **"Invalid or expired Registry JWT token"**: 重新认证 `mcp-publisher login github`
- **"You do not have permission to publish this server"**: 认证方式与命名空间不匹配（如 GitHub 认证必须使用 `io.github.username/*` 格式）

**参考**: [Quickstart - Troubleshooting](https://modelcontextprotocol.io/registry/quickstart#troubleshooting)

---

## 8. 版本管理要点

- **版本格式**: 推荐语义化版本（如 `1.0.0`、`2.1.3-alpha`），支持日期版本（如 `2025.11.25`）
- **禁止范围表示**: 不允许 `^1.2.3`、`~1.2.3`、`>=1.2.3` 等范围表达式
- **最佳实践**:
  - 服务器版本与底层包版本对齐
  - 如需多次发布相同包版本（仅更新元数据），使用预发布版本（如 `1.2.3-1`）
  - 已发布版本无法修改，只能发布新版本

**参考**: [Versioning Guidelines](https://modelcontextprotocol.io/registry/versioning)

---

## 快速检查清单

发布前确认：
- [ ] server.json 的 `name` 格式符合认证方式（GitHub: `io.github.username/*`）
- [ ] 底层包已发布到对应 registry（npm/PyPI/NuGet/OCI）
- [ ] 包中包含必需的验证信息（`mcpName` 或 `mcp-name:`）
- [ ] 已通过 `mcp-publisher login` 认证
- [ ] 版本号唯一且符合语义化版本规范
- [ ] （如需自动化）GitHub Actions workflow 配置正确，包含 OIDC 权限

---

## 官方资源链接

- **Registry 首页**: https://modelcontextprotocol.io/registry/about
- **快速开始**: https://modelcontextprotocol.io/registry/quickstart
- **包类型说明**: https://modelcontextprotocol.io/registry/package-types
- **认证方法**: https://modelcontextprotocol.io/registry/authentication
- **版本管理**: https://modelcontextprotocol.io/registry/versioning
- **GitHub Actions**: https://modelcontextprotocol.io/registry/github-actions
- **Schema 定义**: https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/server.schema.json
- **Registry 代码库**: https://github.com/modelcontextprotocol/registry

---

**文档创建时间**: 2025-03-03
**状态**: MCP Registry 当前处于预览阶段，可能发生破坏性变更

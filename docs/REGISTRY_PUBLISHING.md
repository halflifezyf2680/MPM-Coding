# MCP Registry Publishing Guide

> This document explains how MPM integrates with the official MCP Registry for server discovery.

## Overview

MPM supports publishing to the [MCP Registry](https://modelcontextprotocol.io/registry/about), enabling:
- Discovery via `modelcontextprotocol.io` and downstream aggregators
- One-click installation for MCP clients
- Version tracking and metadata management

## Key Files

### 1. `server.template.json`

Template for registry submission. Placeholders are resolved during release:

| Placeholder | Description | Source |
|-------------|-------------|--------|
| `${MCP_SERVER_NAME}` | Registry name (reverse DNS) | `vars.MCP_SERVER_NAME` or default |
| `${VERSION}` | Semantic version | Git tag |
| `${MCPB_DOWNLOAD_URL}` | .mcpb download URL | GitHub Release URL |
| `${MCPB_SHA256}` | SHA-256 hash | Calculated at build time |
| `${GITHUB_REPO_URL}` | Repository URL | `github.server_url` |

Default server name: `io.github.halflifezyf2680/mpm-vibe-coding`

### 2. `mcpb/manifest.json`

MCPB bundle manifest describing the binary server:
- Platform-specific binary paths via `mcp_config.platform_overrides`
- Runtime type: `stdio`
- Entrypoint: `mpm-go`

### 3. `.mcpb` Bundle Structure

```
mpm-vibe-coding-v1.0.0.mcpb
├── manifest.json
└── server/
    ├── linux/
    │   ├── mpm-go
    │   └── ast_indexer
    ├── darwin/
    │   ├── mpm-go
    │   └── ast_indexer
    └── win32/
        ├── mpm-go.exe
        └── ast_indexer.exe
```

## Release Workflow

### Automatic (Recommended)

1. Push a version tag: `git tag v1.0.0 && git push origin v1.0.0`
2. `release.yml` builds all platforms, assembles `.mcpb`, uploads to GitHub Release
3. `publish-mcp.yml` triggers on release, publishes to registry via OIDC

### Manual

```bash
# 1. Build and upload release (via workflow_dispatch)
gh workflow run release.yml -f version=v1.0.0

# 2. Publish to registry (via workflow_dispatch)
gh workflow run publish-mcp.yml -f version=v1.0.0
```

## Configuration

### GitHub Variables (Optional)

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_SERVER_NAME` | Registry server name | `io.github.halflifezyf2680/mpm-vibe-coding` |

Set via: Settings → Secrets and variables → Actions → Variables

### Required Permissions

The `publish-mcp.yml` workflow requires:
- `id-token: write` — For GitHub OIDC authentication
- `contents: read` — For repository access

No secrets required (OIDC handles authentication).

## Verification

### Check Registry API

```bash
# Search for the server
curl "https://registry.modelcontextprotocol.io/v0/servers?search=io.github.halflifezyf2680/mpm-vibe-coding"

# Get server details (after publication)
curl "https://registry.modelcontextprotocol.io/v0/servers/{server-id}"
```

### Check Downstream

- [modelcontextprotocol.info](https://modelcontextprotocol.info) — Aggregator mirror
- MCP clients with registry integration

## Troubleshooting

### "Registry validation failed"
- Ensure `.mcpb` URL is publicly accessible
- Verify `fileSha256` matches the uploaded file

### "You do not have permission to publish"
- Server name must match authentication namespace
- GitHub OIDC → `io.github.{username}/*` or `io.github.{orgname}/*`

### "Invalid or expired JWT token"
- Re-run the workflow (OIDC tokens are short-lived)

## References

- [MCP Registry About](https://modelcontextprotocol.io/registry/about)
- [Quickstart Guide](https://modelcontextprotocol.io/registry/quickstart)
- [Package Types](https://modelcontextprotocol.io/registry/package-types)
- [GitHub Actions Automation](https://modelcontextprotocol.io/registry/github-actions)
- [server.json Schema](https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json)

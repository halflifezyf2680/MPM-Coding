# MCP Registry One-Time Publishing Guide

> This document describes how to manually publish to the MCP Registry. This is a one-time operation, not automated in CI.

## Official Resources

- [MCP Registry About](https://modelcontextprotocol.io/registry/about)
- [Registry Quickstart](https://modelcontextprotocol.io/registry/quickstart)
- [GitHub Actions Automation](https://modelcontextprotocol.io/registry/github-actions)
- [server.json Schema](https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json)

## Prerequisites

1. GitHub repository with releases
2. Built binaries for all target platforms
3. `mcp-publisher` CLI tool

## Materials Checklist

- [ ] `server.json` — Registry metadata file
- [ ] Downloadable binary package (`.mcpb` or direct binaries)
- [ ] SHA256 hash of the package
- [ ] Server name in reverse-DNS format: `io.github.{username}/{server-name}`

## Steps

### 1. Install mcp-publisher

```bash
# Linux/macOS
curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar xz mcp-publisher

# Or download from: https://github.com/modelcontextprotocol/registry/releases
```

### 2. Authenticate

```bash
./mcp-publisher login github-oidc
```

This uses your GitHub identity. Your server name must match your namespace:
- Personal: `io.github.{username}/*`
- Organization: `io.github.{orgname}/*`

### 3. Prepare server.json

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
  "name": "io.github.{username}/{server-name}",
  "title": "Your Server Title",
  "description": "Brief description of your MCP server",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "mcpb",
      "identifier": "https://github.com/{username}/{repo}/releases/download/v1.0.0/your-server.mcpb",
      "fileSha256": "sha256-hash-here",
      "transport": {
        "type": "stdio"
      }
    }
  ],
  "repository": {
    "url": "https://github.com/{username}/{repo}",
    "source": "github"
  }
}
```

### 4. Calculate SHA256

```bash
sha256sum your-package.mcpb
# Or on macOS:
shasum -a 256 your-package.mcpb
```

### 5. Publish

```bash
./mcp-publisher publish
```

The tool reads `server.json` from the current directory.

### 6. Verify

```bash
# Query the registry
curl "https://registry.modelcontextprotocol.io/v0.1/servers?search=io.github.{username}"
```

## Notes

- **One-time operation**: This is not automated. Run manually when you want to update registry metadata.
- **Version bumps**: Update `version` in `server.json` and re-publish for each release.
- **Namespace**: Server name must match your GitHub identity (OIDC requirement).
- **Public packages**: All registry entries are public.

## Troubleshooting

| Error | Solution |
|-------|----------|
| "You do not have permission" | Server name must match `io.github.{your-username}/*` |
| "Invalid JWT" | Re-run `mcp-publisher login github-oidc` |
| "URL not accessible" | Ensure release assets are public |

## Reference

For complete documentation, see: https://modelcontextprotocol.io/registry

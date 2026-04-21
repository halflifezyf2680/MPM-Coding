# Security Policy

## Reporting Vulnerabilities

If you discover a security vulnerability, please report it responsibly:

- **Preferred**: Open a [GitHub Issue](https://github.com/halflifezyf2680/MPM-Coding/issues) with the `[Security]` prefix
- Include: description of the vulnerability, steps to reproduce, potential impact

## Security Model

MPM-Coding is a **local-only** MCP Server with a minimal attack surface:

- **Pure StdIO**: No network ports exposed
- **No external API calls**: Zero token consumption, zero data transmission
- **Command injection**: All external commands use `exec.Command` with argument arrays (not shell strings)
- **Path traversal**: Scope parameters are validated via `path_guard.go`

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No |

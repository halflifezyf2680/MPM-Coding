# Contributing to MPM-Coding

Thanks for your interest in contributing!

## Development Setup

1. **Go 1.21+** and **Rust** (for Tree-sitter indexer) required
2. Clone the repo: `git clone https://github.com/halflifezyf2680/MPM-Coding.git`
3. Build Go server: `cd mcp-server-go && go build ./cmd/mpm-go`
4. Build Rust indexer: `cd mcp-server-go/internal/services/ast_indexer_rust && cargo build --release`

## Reporting Issues

- Use [GitHub Issues](https://github.com/halflifezyf2680/MPM-Coding/issues)
- Include: OS, Go version, reproduction steps, expected vs actual behavior

## Pull Requests

1. Fork the repo
2. Create a feature branch: `git checkout -b feature/your-feature`
3. Make changes and add tests where applicable
4. Ensure `go test ./...` passes
5. Open a PR with a clear description of the change

## Code Style

- Follow standard Go conventions (`gofmt`)
- Keep functions focused and small
- Add Chinese descriptions for MCP tool definitions (matches existing style)

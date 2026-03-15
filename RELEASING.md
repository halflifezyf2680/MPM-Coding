# Release Guide

## Pre-flight Checklist

- [ ] Working tree is clean (`git status` shows no uncommitted changes)
- [ ] All tests pass: `cd mcp-server-go && go test ./...`
- [ ] Build succeeds: `cd mcp-server-go && go build ./...`
- [ ] Version follows [SemVer](https://semver.org/)

## Versioning Rules

- **MAJOR** (X.0.0): Breaking API changes
- **MINOR** (0.Y.0): New features, backward compatible
- **PATCH** (0.0.Z): Bug fixes, backward compatible

## Release Process

### 1. Tag and Push

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow triggers automatically on tag push.

### 2. Verify Release

Check the [Actions](../../actions) tab for workflow status. On success:
- GitHub Release is created with platform binaries
- Assets include SHA256SUMS for verification

## If Workflow Fails

1. **Do NOT delete or re-tag** — tags are immutable in public repos
2. Fix the issue locally
3. Bump the **patch** version and release again:

```bash
# If v1.2.3 failed, release v1.2.4
git add .
git commit -m "fix: <description>"
git tag v1.2.4
git push origin main v1.2.4
```

## Hotfix Rule

- **Never retag** — always bump version
- Failed release = new patch version
- No exceptions

## Release Assets

| Platform | Archive |
|----------|---------|
| Windows x64 | `mpm-windows-amd64.zip` |
| Linux x64 | `mpm-linux-amd64.tar.gz` |
| macOS Universal | `mpm-darwin-universal.tar.gz` |

## Verification

```bash
# Download SHA256SUMS from release, then:
sha256sum -c SHA256SUMS
```

# CI Debugging (GitHub Actions)

This doc is a quick reference for inspecting Nexus CI runs using the GitHub CLI (`gh`).

## Common commands

```bash
# Recent runs on main
gh run list --branch main --limit 20

# Watch a run (exits non-zero if it fails)
gh run watch <run-id> --compact --interval 15 --exit-status

# Show run conclusion + jobs
gh run view <run-id> --json conclusion,status,jobs --jq '.conclusion, (.jobs[] | {name: .name, databaseId: .databaseId, conclusion: .conclusion})'

# View a specific job log
gh run view --job <job-id> --log
```

## Common gotchas

### 1) Run “failed” but it was actually cancelled

This repo’s CI uses workflow concurrency (`ci-${{ github.ref }}` with `cancel-in-progress: true`), so a new push to the same branch can cancel in-flight runs.

To confirm:

```bash
gh run view <run-id> --json conclusion,status
```

If `conclusion` is `cancelled`, it’s not a test failure.

### 2) Go toolchain / cache restore tar errors

If `actions/setup-go` installs `go` from `go.mod` and the module uses a `toolchain goX.Y.Z` directive, `go` may download the toolchain module during the setup step. In some cases this collides with the module cache restore and produces tar errors like `Cannot open: File exists`.

The CI workflow avoids this by explicitly installing the toolchain Go version (parsed from `go.mod`) in `actions/setup-go` rather than using `go-version-file`.


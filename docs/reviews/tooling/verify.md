# Verify: Coding standards and tooling

Updated: 2026-10-11.

You can use this checklist to repeat the local tooling checks. It derives from [scope feature 2](../../scope/scope.md) and the selected gate in [AGENTS.md](../../../AGENTS.md). Tooling has no separate architecture spec. The criterion labels below are local to this checklist.

## Criteria

1. AC-1: The hook and lint configurations are valid for the tested tools.
2. AC-2: Formatting failures block commits, report affected files, and leave source unchanged.
3. AC-3: Vet, race tests, and the selected lint checks pass across the module.
4. AC-4: The installed commit hook runs the gate and stops at its first failure.

## Commands

You can run these from the repository root with Go `1.27.2`, `editorconfig-checker 4.0.2`, `golangci-lint 2.14.0`, and `pre-commit 4.6.2` available on your `PATH`.

- [x] `rtk proxy pre-commit validate-config` exits successfully (AC-1).
- [x] `rtk proxy golangci-lint config verify` exits successfully (AC-1).
- [x] `rtk proxy sh scripts/check-format.sh` exits successfully with no output (AC-2).
- [x] `rtk proxy pre-commit run --all-files` reports Passed for Go formatting and EditorConfig, Go vet, Go race tests, and Go lint (AC-3, AC-4).
- [x] `rtk proxy go test ./scripts` exercises the real formatting gate with a long line that passes gofmt, the exact 120-character boundary, smaller and larger EditorConfig limits, script Go files, and raw string indentation (AC-2).

If you need to isolate a failure, you can run `rtk proxy go vet ./...`, `rtk proxy go test -race ./...`, or `rtk proxy golangci-lint run ./...` separately. The lint policy enables `errcheck`, `govet`, `ineffassign`, `staticcheck`, and `unused`.

## Hook failure behavior

You can exercise these cases in a temporary Git repository to preserve your working files. Copy `.editorconfig`, `.pre-commit-config.yaml`, and `scripts/check-format.sh` into it. Create `cmd/main.go` and `internal/example.go`, then stage the configuration, script, and source files. You can use a temporary `PRE_COMMIT_HOME` directory for the hook cache.

- [x] With `package main` followed by `func main(){ }` in `cmd/main.go`, install the hook using `rtk proxy pre-commit install` and attempt a temporary commit with a local test identity. The commit fails, the output names `cmd/main.go`, no commit is created, the file bytes remain unchanged, and later hooks do not run (AC-2, AC-4).
- [x] After `rtk proxy gofmt -w cmd/main.go`, `rtk proxy pre-commit run gofmt --all-files` reports Passed (AC-2).
- [x] With invalid Go syntax in the fixture, `rtk proxy sh scripts/check-format.sh` exits with an error (AC-2).

For a complete gate check in the temporary repository, you can copy the real module files, source tree, and `.golangci.yml` into it. After staging them and creating a baseline commit, a commit changing only a documentation file should still run all four hooks (AC-4).

## Recorded development results

On 2026-10-10, both configuration validators and all four gate checks passed on macOS arm64. The hook was installed in the project checkout. A temporary repository confirmed that an actual commit with bad formatting was rejected, source bytes were preserved, later hooks stopped, formatted input passed, and invalid Go syntax returned an error.

On 2026-10-11, GoLand `2026.2.0.1` applied the exported EditorConfig settings to all 59 Go files. Long literals and conditions were then split while preserving their values. All Go files passed both gofmt and EditorConfig, with no lines wider than 120 columns at tab width 4. A syntax-tree comparison with adjacent string constants folded confirmed unchanged expressions and string values in all 58 files outside the expanded formatting regression suite. The full formatting, vet, race-test, and lint gate passed on macOS arm64 with toolchain and cache access. `scripts/format.sh` runs GoLand, gofmt, and the EditorConfig check in that order; the commit gate only checks and does not need GoLand installed.

The documentation only commit scenario is a repeatable follow up check; it was not separately exercised during development. These results do not establish Linux runtime behavior or CI execution. CI remains pending provider selection. Saving this checklist does not mark the feature done or create a commit.

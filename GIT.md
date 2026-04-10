# Git Workflow for plugin-esphome

This repository contains the Slidebolt ESPHome Plugin, providing integration with ESPHome-flashed devices using the Native API. It produces a standalone binary.

## Dependencies
- **Internal:**
  - `sb-contract`: Core interfaces.
  - `sb-domain`: Shared domain models.
  - `sb-logging`: Logging implementation.
  - `sb-logging-sdk`: Logging client interfaces.
  - `sb-messenger-sdk`: Shared messaging interfaces.
  - `sb-runtime`: Core execution environment.
  - `sb-script`: Scripting engine.
  - `sb-storage-sdk`: Shared storage interfaces.
  - `sb-testkit`: Testing utilities.
  - `sb-virtual`: Virtual device provider.
- **External:** 
  - `github.com/mycontroller-org/esphome_api`: ESPHome Native API client.
  - `github.com/grandcat/zeroconf`: mDNS/ZeroConf discovery.
  - `github.com/cucumber/godog`: BDD testing framework.

## Build Process
- **Type:** Go Application (Plugin).
- **Consumption:** Run as a background plugin service.
- **Artifacts:** Produces a binary named `plugin-esphome`.
- **Command:** `go build -o plugin-esphome ./cmd/plugin-esphome`
- **Validation:** 
  - Validated through unit tests: `go test -v ./...`
  - Validated through BDD tests: `go test -v ./cmd/plugin-esphome`
  - Validated by successful compilation of the binary.

## Pre-requisites & Publishing
As a complex hardware integration plugin, `plugin-esphome` must be updated whenever any of the core SDKs or implementation services are changed.

**Before publishing:**
1. Determine current tag: `git tag | sort -V | tail -n 1`
2. Ensure all local tests pass: `go test -v ./...`
3. Ensure the binary builds: `go build -o plugin-esphome ./cmd/plugin-esphome`

**Publishing Order:**
1. Ensure all internal dependencies are tagged and pushed.
2. Update `plugin-esphome/go.mod` to reference the latest tags.
3. Determine next semantic version for `plugin-esphome` (e.g., `v1.0.4`).
4. Commit and push the changes to `main`.
5. Tag the repository: `git tag v1.0.4`.
6. Push the tag: `git push origin main v1.0.4`.

## Update Workflow & Verification
1. **Modify:** Update ESPHome integration logic in `internal/` or `app/`.
2. **Verify Local:**
   - Run `go mod tidy`.
   - Run `go test ./...`.
   - Run `go test ./cmd/plugin-esphome` (BDD features).
   - Run `go build -o plugin-esphome ./cmd/plugin-esphome`.
3. **Commit:** Ensure the commit message clearly describes the change.
4. **Tag & Push:** (Follow the Publishing Order above).

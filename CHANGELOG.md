# Changelog

All notable changes to this project will be documented in this file.

## [1.1.0] - 2026-01-18

### Added
- Tilde (`~`) path expansion support for store directories in `config.toml` and CLI flags.
- Enhanced `init` command with store existence check, auto-creation prompt, and validity verification.
- Overlap prevention: `init` now blocks if source and store directories overlap.
- Count of ignored files and directories in backup summary.
- Support for `--show-ignored` in `create` command to list skipped files during backup.
- Global exclude patterns in `config.toml`.
- Automatic creation of `README.md` files in backup sources and stores for better usability.
- GitHub Actions workflow for cross-OS testing (Linux, macOS, Windows).

### Changed
- Renamed `backup` command to `create` for clarity, with `backup` retained as an alias for backward compatibility.
- Refactored directory structure: Moved source files from `internal/backup` to `internal` to avoid naming conflicts.
- Flattened project structure: Moved `cmd/backup/*` to project root to simplify installation (`go install` from root).
- Removed legacy "Java" related TODOs and comments from the codebase.

### Fixed
- Windows compatibility: Fixed file locking issues during backup save operations.
- Windows compatibility: Fixed integration tests to verify executable path with `.exe` extension.
- Windows compatibility: Fixed path separator handling in ignore patterns (gitignore/backupignore).
- Windows compatibility: Fixed TOML configuration generation to properly escape Windows paths.
- Windows compatibility: Fixed headless snapshot listing by parsing project names from directory structure.

## [1.0.0] - 2025-12-25

### Initial release

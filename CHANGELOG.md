# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- Automatic creation of `README.md` files in backup sources and stores for better usability.
- GitHub Actions workflow for cross-OS testing (Linux, macOS, Windows).

### Fixed
- Windows compatibility: Fixed file locking issues during backup save operations.
- Windows compatibility: Fixed integration tests to verify executable path with `.exe` extension.
- Windows compatibility: Fixed path separator handling in ignore patterns (gitignore/backupignore).
- Windows compatibility: Fixed TOML configuration generation to properly escape Windows paths.
- Windows compatibility: Fixed headless snapshot listing by parsing project names from directory structure.

### Changed
- Flattened project structure: Moved `cmd/backup/*` to project root to simplify installation (`go install` from root).
- Removed legacy "Java" related TODOs and comments from the codebase.

## [1.0.0] - 2025-12-25

### Initial release

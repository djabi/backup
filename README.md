# Go Backup Tool

A simple and efficient incremental backup tool written in Go.

## Overview

This tool allows you to create snapshots of a directory structure, storing them in a content-addressable storage format. It supports incremental backups (deduplication), **symbolic links**, listing backup history, inspecting backup contents, checking the status of your current workspace against the latest backup, and **hash cache verification** for data integrity.

## Cross-Platform Support

This tool supports **Linux**, **macOS**, and **Windows**.

### Limitations
- **Windows Symbolic Links**: Symbolic link support on Windows depends on developer mode or administrative privileges. If the tool lacks permission to create symlinks during restore, it may fail or skip them.
- **File Permissions**: Unix-style file permissions (chmod) are preserved but may not map perfectly to Windows ACLs.
- **Path Separators**: The tool automatically handles path separators, but when specifying paths in configuration files manually, use forward slashes `/` or escaped backslashes `\\` to ensure compatibility.

## Installation

To install the tool, use `go install` from the project root:

```bash
go install
```

This will compile the project and install the binary as `backup` (or `backup.exe` on Windows) to your `$GOPATH/bin` (or `$GOBIN`). Ensure this directory is in your system's `PATH`.

## Store Structure
The backup store uses a Content-Addressable Storage (CAS) model to efficiently deduplicate data.

- `store/data`: Contains the actual file content and directory listings.
  - Blobs are stored as gzipped files.
  - filenames are the MD5 hash of the uncompressed content.
  - Sharded by the first 2 characters of the hash (e.g., `store/data/a1/a1b2c3...`).
- `store/snapshots`: Contains the snapshot references.
  - Organized by project name and timestamp: `store/snapshots/<ProjectName>/<Timestamp>`.
  - Each snapshot file contains the hash of the root directory for that backup.

## Usage

### Configuration

The tool uses a configuration file to locate the backup store and define project settings.

**1. Source Configuration (`.backup/config.toml`)**
Placed in the root of the source tree to be backed up:

```toml
store = "~/path/to/backup/store"  # Supports ~ expansion
name = "My Backup Project"
```

**2. Store Configuration (`.backup/store.toml`)**
Placed in the root of the backup store. This file is automatically created when you initialize a store (e.g., `backup --store ./my-store ...`). It allows specific CLI commands to run from within the store directory without specifying the `--store` flag.

### Ignoring Files

The tool supports ignoring files and directories using `.gitignore` and `.backupignore` files.

- It respects standard `.gitignore` patterns.
- It also looks for `.backupignore` files.
- `.backupignore` takes precedence over `.gitignore` if both exist in the same directory.
- These files are respected recursively.

### Commands

#### Initialization

To initialize a new backup store:

```bash
backup init-store [path]
```

This will also generate a `README.md` in the store directory with usage instructions.


To initialize a new source directory (project):

```bash
backup init [path] --store <store-path> --project <project-name>
```

This will configure the directory as a backup source and generate a `README.md` in the `.backup` directory.


If flags are omitted, the tool will prompt interactively.

#### Create a Backup

To create a new backup snapshot:

```bash
backup create
# or (legacy)
backup backup
```

Use `--dry-run` to simulate the backup without writing any changes. Use `--show-ignored` to list files and directories skipped by ignore rules.

#### List Snapshots

To list all available backup snapshots:

```bash
backup snapshots
# or
backup snapshot
```

#### List Snapshot Contents

To list the contents of the latest backup:

```bash
backup tree
```

To list the contents of a specific backup:

```bash
backup tree <timestamp>
```

### `Check Status`

To see what has changed in your working directory compared to the latest backup:

```bash
backup status
```

- **Source Mode**: Shows files changed, new, or missing since the last backup. Output is sorted alphabetically. Use `--show-ignored` to see files skipped by ignore rules.
- **Headless Mode**: Lists all projects in the store, sorted by recency, with smart relative timestamps (e.g., "Just now", "2 hours ago").

#### `Restore Backup`

To restore files from a snapshot:

```bash
backup restore <snapshot> [path] [destination]
```

- If running from source directory: destination defaults to current directory.
- If running from source directory: destination defaults to current directory.
- If running from store directory (headless): **destination is strict**. You must provide a destination path, otherwise the command will fail with an error.
- `[path]` (optional): Restore a specific file or directory from the snapshot.

#### `Check Store Integrity`

To verify the integrity of the backup store:

```bash
backup check
```

- `--deep`: Perform a deep check by verifying content hashes (slower).

The `check` command verifies:
- Store structure integrity
- Blob references and reachability
- Hash cache integrity (when run from a source directory)
- Content hash validation (with `--deep` flag)

#### `Prune Store`

To remove unreferenced blobs and reclaim disk space:

```bash
backup prune
```

- `--dry-run`: Show what would be deleted without actually removing any files.
The command also scans for and reports unreferenced blobs (blobs not referenced by any existing snapshot). If unreferenced blobs are found, the check will fail. You can use the `prune` command to remove them.

#### `Prune Hash Cache`

To clean up stale entries in the local hash cache (for files that no longer exist):

```bash
backup prune-cache
```

- `--dry-run`: Show what would be removed without actually removing anything.

*Note: The `backup` command now automatically performs this cleanup, but this command can be used for manual maintenance.*

#### `Version`

To display the tool version:

```bash
backup version
# or
backup --version
```

#### `Remove Snapshot`

To delete specific backup snapshots (e.g. to save space or remove sensitive data):

```bash
backup remove <snapshot-id> [snapshot-id...]
# Aliases: rm, forget, delete
```

The command automatically runs a `prune` operation afterwards to reclaim space used by the deleted snapshots' unique data.
Use `--dry-run` to see what would be removed without applying changes.

### Flags

- `--root <path>`, `-d <path>`: Specify the root directory of the source to backup. Useful if running the tool from outside the source directory.
- `--store <path>`, `-s <path>`: Specify the backup store directory directly. Useful for inspecting backups without needing a source directory.
- `--yes`, `-y`: Automatically answer "yes" to prompts (e.g., confirming creation of `store.toml` when initializing a new store).
- `--dry-run`: (For `backup` and `prune` commands) Perform a dry run without modifying the store.

## Development

- **Build**: `go build -o backup`
- **Test**: `go test ./...`

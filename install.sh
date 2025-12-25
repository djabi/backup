#!/bin/bash
set -o nounset # exit on use of unassigned var
set -o errexit # exit if command fails.
#set -o verbose # print lines
#set -o xtrace  # print expanded lines
set -e

script_name=$(basename "$0")
script_dir=$(cd "$(dirname "$0")"; pwd)
script="$script_dir/$script_name"


echo "Installing backup tool as backup-cli..."
GOBIN=$(go env GOBIN)
if [ -z "$GOBIN" ]; then
    GOBIN=$(go env GOPATH)/bin
fi
cd "$script_dir"
go build -o "$GOBIN/backup-cli" ./cmd/backup

echo "Successfully installed backup to $GOBIN/backup-cli"

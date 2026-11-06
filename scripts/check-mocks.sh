#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mock_check_dir=$(mktemp -d)
trap 'rm -rf "$mock_check_dir"' EXIT HUP INT TERM
cd "$repo_root/internal/domain/repository"
go tool mockgen -source=device.go -destination="$mock_check_dir/device_mock.go" -package=mock -self_package=smapass-back/internal/domain/repository/mock -write_command_comment=false
if ! cmp -s mock/device_mock.go "$mock_check_dir/device_mock.go"; then
  echo 'Mocks are out of date: run make generate' >&2
  exit 1
fi

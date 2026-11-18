#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 hourinX
#
# Build the release archives attached to a GitHub release.
# Usage: scripts/release.sh <version>   e.g. scripts/release.sh 0.1.0

set -euo pipefail

version="${1:-}"
if [ -z "$version" ]; then
  echo "usage: $0 <version>   e.g. $0 0.1.0" >&2
  exit 1
fi

cd "$(dirname "$0")/.."
out="dist"
rm -rf "$out"
mkdir -p "$out"

# Targets are kept to the ones whose behaviour the project actually supports.
targets="windows/amd64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"

for target in $targets; do
  os="${target%/*}"
  arch="${target#*/}"
  name="virelz_${version}_${os}_${arch}"
  bin="virelz"
  if [ "$os" = "windows" ]; then
    bin="virelz.exe"
  fi

  echo "building ${name}"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/hourinX/Virelz/internal/app.version=${version}" \
    -o "$out/$name/$bin" ./cmd/virelz

  if [ "$os" = "windows" ]; then
    # zip is not always present on Windows; PowerShell ships with every install.
    if command -v zip >/dev/null 2>&1; then
      (cd "$out/$name" && zip -q "../${name}.zip" "$bin")
    else
      powershell -NoProfile -Command \
        "Compress-Archive -Path '$out/$name/$bin' -DestinationPath '$out/${name}.zip' -Force"
    fi
  else
    tar -czf "$out/${name}.tar.gz" -C "$out/$name" "$bin"
  fi
  rm -rf "$out/$name"
done

echo
echo "artifacts in $out:"
ls -1 "$out"
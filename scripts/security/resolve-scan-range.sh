#!/usr/bin/env bash
# Print one git log range for a GitHub Actions push or pull_request scan.
# Usage: resolve-scan-range.sh <head-sha> <base-sha>
#
# A missing, all-zero, or unreachable base happens for a new branch and after
# a history rewrite removes the previous push's base commit. Scanning every
# ancestor would re-check historical commits that are intentionally unchanged.
# Scan only the pushed tip in that case. git log then receives <head>^!.
set -euo pipefail

head="${1:-}"
base="${2:-}"
zero="0000000000000000000000000000000000000000"

if [[ -z "${head}" ]]; then
  echo "resolve-scan-range: missing head sha" >&2
  exit 2
fi

if ! git cat-file -e "${head}^{commit}" >/dev/null 2>&1; then
  echo "resolve-scan-range: head commit is not in this clone" >&2
  exit 2
fi

if [[ -n "${base}" && "${base}" != "${zero}" ]] && git cat-file -e "${base}^{commit}" >/dev/null 2>&1; then
  printf '%s\n' "${base}..${head}"
  exit 0
fi

printf '%s\n' "${head}^!"

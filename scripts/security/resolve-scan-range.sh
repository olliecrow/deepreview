#!/usr/bin/env bash
# Print the scan base for a push range.
# Usage: resolve-scan-range.sh <head-sha> <base-sha>
#
# Prints the base when it is a commit in this clone. A base is empty, all
# zeros, or missing for a new branch or a push after a history rewrite. Then
# it prints the commit in scripts/security/reviewed-base when that commit is
# an ancestor of the head: history up to it was reviewed, and older commits
# hold synthetic test fixtures that the current rules report. Otherwise it
# prints nothing, and the scanners scan all history and fail closed.
# After a history rewrite, put the new reviewed tip in reviewed-base.
set -euo pipefail

head="${1:-}"
base="${2:-}"
zero="0000000000000000000000000000000000000000"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

is_commit() {
  [[ "$1" =~ ^[0-9a-f]{40}$ && "$1" != "${zero}" ]] && git cat-file -e "$1^{commit}" 2>/dev/null
}

if ! is_commit "${head}"; then
  echo "resolve-scan-range: head is not a commit in this clone" >&2
  exit 2
fi

if is_commit "${base}"; then
  printf '%s\n' "${base}"
  exit 0
fi

reviewed="$(tr -d '[:space:]' <"${script_dir}/reviewed-base")"
if is_commit "${reviewed}" && git merge-base --is-ancestor "${reviewed}" "${head}"; then
  printf '%s\n' "${reviewed}"
fi

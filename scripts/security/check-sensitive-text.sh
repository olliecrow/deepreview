#!/usr/bin/env bash
set -euo pipefail

context="text"
if [[ "${1:-}" == --context=* ]]; then
  context="${1#--context=}"
  shift
fi

if [[ "$#" -lt 1 ]]; then
  echo "usage: check-sensitive-text.sh [--context=<label>] <file> [file...]" >&2
  exit 2
fi

# Placeholder allowlists are anchored and tested against each match on its
# own, so a longer name that starts with a placeholder, or a real path beside
# a placeholder on the same line, is still reported.
local_path_regex='(/Users/[A-Za-z0-9._-]+|/home/[A-Za-z0-9._-]+|[A-Za-z]:\\+[Uu][Ss][Ee][Rr][Ss]\\+[A-Za-z0-9._-]+)'
allowed_path_placeholder_regex='^(/Users/(YOU|USER|username)|/home/(user|USER|username)|[A-Za-z]:\\+[Uu][Ss][Ee][Rr][Ss]\\+(YOU|USER|USERNAME|username))$'
secret_assignment_regex='([Aa][Pp][Ii][_-]?[Kk][Ee][Yy]|[Tt][Oo][Kk][Ee][Nn]|[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Ss][Ee][Cc][Rr][Ee][Tt])[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9_./+=-]{12,}'
email_regex='[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+\.)+[A-Za-z]{2,}'
allowed_placeholder_email_regex='@([A-Za-z0-9-]+\.)*example\.(com|org|net)$'
# The left boundary keeps a token prefix from matching inside a word such as
# task-list or risk-assessment.
known_token_regex='(^|[^A-Za-z0-9_-])((ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|sk-(ant-(oat01|api03|admin01)-|proj-|svcacct-|admin-|None-)[A-Za-z0-9_-]{20,}|sk-[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{3,}\.[A-Za-z0-9_-]{16,})|-----BEGIN [A-Z ]*PRIVATE KEY-----'

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT

# Drop NUL bytes and replace other non-text bytes, so rg and grep never treat
# the input as binary and skip it. Line numbers are unchanged.
normalize_bytes() {
  LC_ALL=C tr -d '\000' <"$1" | LC_ALL=C tr -c '\11\12\40-\176' '?' >"$2"
}

collect_findings() {
  local rule_id="$1"
  local pattern="$2"
  local allow_pattern="$3"
  local file_path="$4"
  local search_path="$5"
  local status=0
  local output=""
  if command -v rg >/dev/null 2>&1; then
    output="$(rg --no-filename --line-number --only-matching --no-heading --color never -e "$pattern" -- "$search_path")" || status=$?
  else
    output="$(grep -anEo "$pattern" -- "$search_path")" || status=$?
  fi
  if [[ "$status" -eq 1 ]]; then
    return 0
  fi
  if [[ "$status" -ne 0 ]]; then
    echo "sensitive-text search failed: ${rule_id} ${file_path}" >&2
    return 1
  fi

  local match_line line_no match
  while IFS= read -r match_line; do
    [[ -z "$match_line" ]] && continue
    line_no="${match_line%%:*}"
    match="${match_line#*:}"
    if [[ ! "$line_no" =~ ^[0-9]+$ ]]; then
      echo "sensitive-text search returned an unexpected record: ${rule_id} ${file_path}" >&2
      return 1
    fi
    if [[ -n "$allow_pattern" ]] && printf '%s\n' "$match" | grep -Eq "$allow_pattern"; then
      continue
    fi
    printf '%s %s:%s\n' "$rule_id" "$file_path" "$line_no"
  done <<<"$output"
}

record_rule() {
  local status=0
  local result=""
  result="$(collect_findings "$1" "$2" "$3" "$4" "$5")" || status=$?
  if [[ "$status" -ne 0 ]]; then
    search_failed=1
    return 0
  fi
  findings+="${result}"$'\n'
}

failed=0
for target in "$@"; do
  if [[ ! -f "$target" || ! -r "$target" ]]; then
    echo "sensitive-text input unavailable: ${target}" >&2
    failed=1
    continue
  fi

  findings=""
  search_failed=0
  normalized="${tmp_dir}/normalized.txt"
  if ! normalize_bytes "$target" "$normalized"; then
    echo "sensitive-text input unreadable: ${target}" >&2
    failed=1
    continue
  fi
  record_rule local-path "$local_path_regex" "$allowed_path_placeholder_regex" "$target" "$normalized"
  record_rule local-email "$email_regex" "$allowed_placeholder_email_regex" "$target" "$normalized"
  record_rule secret-assignment "$secret_assignment_regex" "" "$target" "$normalized"
  record_rule known-token "$known_token_regex" "" "$target" "$normalized"

  if [[ "$search_failed" -ne 0 ]]; then
    failed=1
  fi
  findings="$(printf '%s\n' "$findings" | sed '/^$/d' | sort -u)"
  if [[ -n "$findings" ]]; then
    echo "policy violation in ${context}: ${target}" >&2
    printf '%s\n' "$findings" >&2
    failed=1
  fi
done

if [[ "$failed" -ne 0 ]]; then
  cat >&2 <<'EOF'
Blocked by sensitive-text policy.
- Remove or redact secrets and credential-like values.
- Replace local absolute paths with repo-relative paths or placeholders like /path/to/project.
EOF
fi

exit "$failed"

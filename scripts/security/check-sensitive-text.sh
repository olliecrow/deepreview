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

local_path_regex='(/Users/[A-Za-z0-9._-]+|/home/[A-Za-z0-9._-]+|[A-Za-z]:\\+Users\\+[A-Za-z0-9._-]+)'
allowed_path_placeholder_regex='(/Users/(YOU|USER|username)|/home/(user|USER|username)|[A-Za-z]:\\+Users\\+(YOU|USER|USERNAME|username))'
secret_assignment_regex='([Aa][Pp][Ii][_-]?[Kk][Ee][Yy]|[Tt][Oo][Kk][Ee][Nn]|[Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Ss][Ee][Cc][Rr][Ee][Tt])[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9_./+=-]{12,}'
email_regex='[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+\.)+[A-Za-z]{2,}'
allowed_placeholder_email_regex='@([A-Za-z0-9._%+-]+\.)?example\.(com|org|net)([^A-Za-z0-9]|$)'
known_token_regex='((ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|sk(-[A-Za-z0-9]+)?-[A-Za-z0-9]{20,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|xox[baprs]-[A-Za-z0-9-]{10,})'

collect_findings() {
  local rule_id="$1"
  local pattern="$2"
  local allow_pattern="$3"
  local file_path="$4"
  local status=0
  local output=""
  if command -v rg >/dev/null 2>&1; then
    output="$(rg --no-filename --line-number --only-matching --no-heading --color never -e "$pattern" -- "$file_path")" || status=$?
  else
    output="$(grep -nEo "$pattern" -- "$file_path")" || status=$?
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
  result="$(collect_findings "$1" "$2" "$3" "$4")" || status=$?
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
  record_rule local-path "$local_path_regex" "$allowed_path_placeholder_regex" "$target"
  record_rule local-email "$email_regex" "$allowed_placeholder_email_regex" "$target"
  record_rule secret-assignment "$secret_assignment_regex" "" "$target"
  record_rule known-token "$known_token_regex" "" "$target"

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

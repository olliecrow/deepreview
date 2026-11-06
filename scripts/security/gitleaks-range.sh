#!/usr/bin/env bash
# Run gitleaks on a commit range and fail closed.
# gitleaks can log a git error, scan 0 commits, and still exit 0 with
# "no leaks found". So git checks the range first, and the scan fails on
# any logged error or when it scanned fewer commits than git says it must.
# Exits 1 on a leak and 2 on any error.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: gitleaks-range.sh [--from <sha>] --to <sha>

Scans every commit in <from>..<to> in the current repository with the
gitleaks config of this repository. Without --from, or with an all-zero
--from, scans all commits reachable from <to>. Merge commits are scanned
against their first parent. Each commit message is scanned too.
EOF
}

zero_ref="0000000000000000000000000000000000000000"
object_id_regex='^([0-9a-f]{40}|[0-9a-f]{64})$'
config="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/.gitleaks.toml"

die() {
  echo "error: $*" >&2
  exit 2
}

from_ref=""
to_ref=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --from | --to)
      [[ "$#" -ge 2 ]] || { usage; exit 2; }
      if [[ "$1" == "--from" ]]; then from_ref="$2"; else to_ref="$2"; fi
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

[[ "$to_ref" =~ $object_id_regex ]] || die "scan target is not a full commit ID"
git rev-parse --verify --quiet "${to_ref}^{commit}" >/dev/null ||
  die "scan target is not a commit in this repository"
if [[ -z "$from_ref" || "$from_ref" == "$zero_ref" ]]; then
  range="$to_ref"
else
  [[ "$from_ref" =~ $object_id_regex ]] || die "scan base is not a full commit ID"
  git rev-parse --verify --quiet "${from_ref}^{commit}" >/dev/null ||
    die "scan base is not a commit in this repository"
  range="${from_ref}..${to_ref}"
fi

commit_count="$(git rev-list --count "$range")" || die "unable to list commits"
if [[ "$commit_count" -eq 0 ]]; then
  echo "no commits to scan"
  exit 0
fi

# gitleaks does not count a commit that only removes lines, so the floor is
# the number of commits that add at least one line.
diff_opts=(--text --no-ext-diff --no-textconv --diff-merges=first-parent)
commits_with_added_lines="$(
  git --no-pager log --root --format='format:commit %H' --numstat --no-renames \
    "${diff_opts[@]}" "$range" |
    awk '/^commit / { if (added > 0) count++; added = 0; next }
         NF >= 3 { added += $1 }
         END { if (added > 0) count++; print count + 0 }'
)" || die "unable to count commits with added lines"

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT
raw_log="${tmp_dir}/raw.log"
log_file="${tmp_dir}/gitleaks.log"

status=0
gitleaks git --no-banner --redact --verbose --config "$config" \
  --log-opts="${diff_opts[*]} ${range}" . >"$raw_log" 2>&1 || status=$?
# gitleaks writes color codes even to a file.
sed "s/$(printf '\033')\[[0-9;]*m//g" "$raw_log" >"$log_file"
cat "$log_file"

[[ "$status" -ne 1 ]] || exit 1
[[ "$status" -eq 0 ]] || die "gitleaks exited with status ${status}"
if grep -qE '(^| )(ERR|FTL) ' "$log_file"; then
  die "gitleaks logged an error"
fi
scanned="$(sed -n 's/.* \([0-9][0-9]*\) commits scanned\..*/\1/p' "$log_file")"
[[ "$scanned" =~ ^[0-9]+$ ]] || die "gitleaks did not report a commit count"
if [[ "$scanned" -lt "$commits_with_added_lines" ]]; then
  die "gitleaks scanned ${scanned} commits, expected at least ${commits_with_added_lines}"
fi

# gitleaks git scans patches only. Default rules know secret types the shell
# policy does not, so each commit message is scanned too.
message_log="${tmp_dir}/message.log"
commits="$(git rev-list "$range")" || die "unable to list commits"
leaked=0
while IFS= read -r commit; do
  status=0
  git --no-pager log -1 --encoding=UTF-8 --no-notes --format='%B' "$commit" |
    gitleaks stdin --no-banner --no-color --redact --verbose --config "$config" >"$message_log" 2>&1 ||
    status=$?
  if grep -qE '(^| )(ERR|FTL) ' "$message_log"; then
    die "gitleaks logged an error scanning the message of ${commit}"
  fi
  case "$status" in
    0) ;;
    1)
      echo "leak in the commit message of ${commit}:"
      cat "$message_log"
      leaked=1
      ;;
    *) die "gitleaks exited with status ${status} scanning the message of ${commit}" ;;
  esac
done <<<"$commits"
[[ "$leaked" -eq 0 ]] || exit 1
echo "scanned ${commit_count} commit messages"

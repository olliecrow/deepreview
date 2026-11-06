#!/usr/bin/env bash
# Scan commit messages, added lines, and paths in a push range.
# Range: PRE_COMMIT_FROM_REF..PRE_COMMIT_TO_REF (set by pre-commit pre-push,
# CI, and deepreview delivery). An empty or all-zero base scans every commit
# reachable from the target that is not already on PRE_COMMIT_REMOTE_NAME.
# Without a remote name it scans from scripts/security/reviewed-base (see
# resolve-scan-range.sh). Any git failure exits non-zero, so an error never
# passes.
set -euo pipefail

zero_ref="0000000000000000000000000000000000000000"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() {
  echo "check-push-range: $*" >&2
  exit 2
}

resolve_commit() {
  [[ "$1" != -* ]] || die "$2 is not a commit"
  git rev-parse --verify --quiet "$1^{commit}" || die "$2 is not a commit"
}

from_ref="${PRE_COMMIT_FROM_REF:-}"
to_ref="${PRE_COMMIT_TO_REF:-}"
remote_name="${PRE_COMMIT_REMOTE_NAME:-}"

[[ -n "$to_ref" ]] || die "PRE_COMMIT_TO_REF is not set"
# An all-zero target deletes a ref. Nothing new is sent.
[[ "$to_ref" != "$zero_ref" ]] || exit 0
to_sha="$(resolve_commit "$to_ref" "push target")"

if [[ -z "$from_ref" || "$from_ref" == "$zero_ref" ]]; then
  if [[ -n "$remote_name" ]]; then
    [[ "$remote_name" =~ ^[A-Za-z0-9._-]+$ ]] || die "invalid remote name"
    commit_list="$(git rev-list "$to_sha" --not --remotes="$remote_name")" ||
      die "unable to list commits for the push range"
  else
    from_sha="$(bash "${script_dir}/resolve-scan-range.sh" "$to_sha" "")" || die "unable to resolve the scan base"
    if [[ -n "$from_sha" ]]; then
      commit_list="$(git rev-list "${from_sha}..${to_sha}")" || die "unable to list commits for the push range"
    else
      commit_list="$(git rev-list "$to_sha")" || die "unable to list commits for the push range"
    fi
  fi
else
  from_sha="$(resolve_commit "$from_ref" "push base")"
  commit_list="$(git rev-list "${from_sha}..${to_sha}")" || die "unable to list commits for the push range"
fi
[[ -n "$commit_list" ]] || exit 0

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "${tmp_dir}"' EXIT

commit_msg_file="${tmp_dir}/commit_messages.txt"
paths_file="${tmp_dir}/changed_paths.txt"
patch_dir="${tmp_dir}/added"
mkdir "$patch_dir"

# Explicit options override user config such as log.showRoot or
# log.diffMerges. Forced text and disabled drivers keep NUL bytes and -diff
# attributes from hiding content. Merges are diffed against the first parent.
printf '%s\n' "$commit_list" |
  git --no-pager log --stdin --no-walk=unsorted --encoding=UTF-8 --no-notes \
    --format='%H%n%s%n%b%n' >"$commit_msg_file" || die "unable to read commit messages"

printf '%s\n' "$commit_list" |
  git --no-pager -c core.quotePath=false diff-tree --stdin --root -r \
    --diff-merges=first-parent --no-renames --no-commit-id --name-only >"$paths_file" ||
  die "unable to list changed paths"

# One file per commit, so a block names the commit. Only + lines inside a
# hunk are added content. A "+++" file header is outside every hunk.
added_files=()
while IFS= read -r commit; do
  out="${patch_dir}/${commit}.txt"
  git --no-pager log -1 --root --diff-merges=first-parent --no-renames --no-notes \
    --format= --patch --text --no-color --unified=0 --no-ext-diff --no-textconv "$commit" >"${out}.patch" ||
    die "unable to read the patch for ${commit}"
  awk '/^diff / { in_hunk = 0; next }
       /^@@ / { in_hunk = 1; next }
       in_hunk && /^\+/ { print substr($0, 2) }' "${out}.patch" >"$out"
  rm -- "${out}.patch"
  added_files+=("$out")
done <<<"$commit_list"

status=0
bash "${script_dir}/check-sensitive-text.sh" --context=push-commit-message "$commit_msg_file" || status=1
bash "${script_dir}/check-sensitive-text.sh" --context=push-paths "$paths_file" || status=1
bash "${script_dir}/check-sensitive-text.sh" --context=push-diff "${added_files[@]}" || status=1
exit "$status"

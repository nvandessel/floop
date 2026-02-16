#!/usr/bin/env bash
set -euo pipefail

# Generate and insert a Keep-a-Changelog release section based on conventional commits.
# Usage: bash scripts/generate_changelog.sh <new-version-tag-or-version>
# Example: bash scripts/generate_changelog.sh v0.3.0

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <new-version-tag-or-version>" >&2
  exit 1
fi

raw_version="$1"
version="${raw_version#v}"
tag="v${version}"
release_date="${RELEASE_DATE:-$(date -u +%Y-%m-%d)}"
changelog_file="${CHANGELOG_FILE:-CHANGELOG.md}"
repo_slug="${GITHUB_REPOSITORY:-nvandessel/feedback-loop}"
latest_tag="${FROM_TAG:-$(git tag -l 'v*' --sort=-v:refname | head -n1)}"

if [[ ! -f "$changelog_file" ]]; then
  echo "changelog not found: $changelog_file" >&2
  exit 1
fi

if grep -qE "^## \\[${version//./\\.}\\]" "$changelog_file"; then
  echo "CHANGELOG entry already exists for ${version}; skipping."
  exit 0
fi

if [[ -n "$latest_tag" ]]; then
  mapfile -t subjects < <(git log --pretty='%s' "${latest_tag}..HEAD")
else
  mapfile -t subjects < <(git log --pretty='%s')
fi

strip_prefix() {
  local line="$1"
  echo "$line" | sed -E 's/^[[:alpha:]]+(\([^)]+\))?!?:[[:space:]]*//'
}

normalize_sentence() {
  local line="$1"
  # Ensure leading character is uppercase for consistency.
  first="$(echo "${line:0:1}" | tr '[:lower:]' '[:upper:]')"
  echo "${first}${line:1}"
}

declare -a added fixed changed
for subject in "${subjects[@]}"; do
  subject_lc="$(echo "$subject" | tr '[:upper:]' '[:lower:]')"
  case "$subject_lc" in
    feat:*|feat\(*:*)
      msg="$(strip_prefix "$subject")"
      added+=("$(normalize_sentence "$msg")")
      ;;
    fix:*|fix\(*:*)
      msg="$(strip_prefix "$subject")"
      fixed+=("$(normalize_sentence "$msg")")
      ;;
    docs:*|docs\(*:*|refactor:*|refactor\(*:*|chore:*|chore\(*:*|perf:*|perf\(*:*|style:*|style\(*:*|build:*|build\(*:*|ci:*|ci\(*:*|test:*|test\(*:*)
      msg="$(strip_prefix "$subject")"
      changed+=("$(normalize_sentence "$msg")")
      ;;
    *)
      # Keep non-conventional subjects visible instead of dropping them.
      changed+=("$(normalize_sentence "$subject")")
      ;;
  esac
done

entry_file="$(mktemp)"
awk_file="$(mktemp)"
trap 'rm -f "$entry_file" "$awk_file"' EXIT

{
  echo "## [${version}] - ${release_date}"
  echo

  if [[ ${#added[@]} -gt 0 ]]; then
    echo "### Added"
    echo
    for item in "${added[@]}"; do
      echo "- ${item}"
    done
    echo
  fi

  if [[ ${#fixed[@]} -gt 0 ]]; then
    echo "### Fixed"
    echo
    for item in "${fixed[@]}"; do
      echo "- ${item}"
    done
    echo
  fi

  if [[ ${#changed[@]} -gt 0 ]]; then
    echo "### Changed"
    echo
    for item in "${changed[@]}"; do
      echo "- ${item}"
    done
    echo
  fi

  if [[ ${#added[@]} -eq 0 && ${#fixed[@]} -eq 0 && ${#changed[@]} -eq 0 ]]; then
    echo "### Changed"
    echo
    echo "- Internal maintenance updates."
    echo
  fi
} >"$entry_file"

# Insert release entry after [Unreleased] section and before next version heading.
cat >"$awk_file" <<'AWK'
BEGIN {
  inserted = 0
  in_unreleased = 0
  saw_unreleased = 0
}
{
  if ($0 ~ /^## \[Unreleased\]/) {
    print $0
    print ""
    in_unreleased = 1
    saw_unreleased = 1
    next
  }

  if (in_unreleased == 1) {
    # Replace all previous Unreleased body content with a clean section.
    if ($0 ~ /^## \[/) {
      if (inserted == 0) {
        while ((getline line < ENTRY_FILE) > 0) {
          print line
        }
        close(ENTRY_FILE)
        inserted = 1
      }
      in_unreleased = 0
      print $0
    }
    next
  }

  print $0
}
END {
  if (inserted == 0 && saw_unreleased == 1) {
    if (in_unreleased == 1) {
      while ((getline line < ENTRY_FILE) > 0) {
        print line
      }
      close(ENTRY_FILE)
      inserted = 1
    }
  }

  if (inserted == 0 && saw_unreleased == 0) {
    print ""
    print "## [Unreleased]"
    print ""
    while ((getline line < ENTRY_FILE) > 0) {
      print line
    }
    close(ENTRY_FILE)
  }
}
AWK

awk -v ENTRY_FILE="$entry_file" -f "$awk_file" "$changelog_file" >"${changelog_file}.tmp"
mv "${changelog_file}.tmp" "$changelog_file"

# Update/append reference links.
unreleased_link="[Unreleased]: https://github.com/${repo_slug}/compare/${tag}...HEAD"
version_link="[${version}]: https://github.com/${repo_slug}/releases/tag/${tag}"

if grep -qE '^\[Unreleased\]:' "$changelog_file"; then
  sed -i -E "s|^\[Unreleased\]:.*$|${unreleased_link}|" "$changelog_file"
else
  echo "$unreleased_link" >>"$changelog_file"
fi

if grep -qE "^\\[${version//./\\.}\\]:" "$changelog_file"; then
  sed -i -E "s|^\\[${version//./\\.}\\]:.*$|${version_link}|" "$changelog_file"
else
  echo "$version_link" >>"$changelog_file"
fi

echo "Generated CHANGELOG entry for ${version}."

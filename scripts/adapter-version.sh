#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version_source="${repo_root}/providers/nfeio/adapter/spec.go"

matches="$(
  sed -nE 's/^[[:space:]]*(var[[:space:]]+)?AdapterVersion[[:space:]]*=[[:space:]]*"([^"]+)".*$/\2/p' \
    "${version_source}"
)"
match_count="$(printf '%s\n' "${matches}" | awk 'NF { count++ } END { print count + 0 }')"

if [[ "${match_count}" != "1" ]]; then
  echo "expected exactly one AdapterVersion in ${version_source}; found ${match_count}" >&2
  exit 1
fi

if [[ ! "${matches}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
  echo "AdapterVersion must be a semver without a v prefix; got ${matches}" >&2
  exit 1
fi

printf '%s\n' "${matches}"

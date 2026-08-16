#!/usr/bin/env bash
# Copyright (c) Sergey Petrovsky
# This source code is licensed under the MIT license found in the
# LICENSE file in the root directory of this source tree.

# Extracts the body of a single version section from CHANGELOG.md, for use as
# GitHub release notes. Usage: changelog-notes.sh <version>, where <version>
# is a git tag (e.g. "v0.2.4") or bare version (e.g. "0.2.4").

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $(basename "$0") <version>" >&2
  exit 1
fi

version="${1#v}"
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
changelog="$root_dir/CHANGELOG.md"

awk -v ver="$version" '
  /^## \[/ {
    if (found) exit
    if ($0 ~ ("\\[" ver "\\]")) { found = 1 }
    next
  }
  found && /^---$/ { exit }
  found { lines[++n] = $0 }
  END {
    if (n == 0) {
      print "no CHANGELOG.md section found for version " ver > "/dev/stderr"
      exit 1
    }
    start = 1
    end = n
    while (start <= end && lines[start] == "") start++
    while (end >= start && lines[end] == "") end--
    for (i = start; i <= end; i++) print lines[i]
  }
' "$changelog"

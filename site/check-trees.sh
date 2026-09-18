#!/usr/bin/env bash
# Checks the split between the public tree and the member tree. Run it after
# building both:
#
#   hugo --minify --cleanDestinationDir
#   hugo --minify --cleanDestinationDir --environment members
#   ./check-trees.sh
#
# Every top-level section under content-members/ must be present in
# public-members/, absent from public/, and named nowhere inside public/. The
# section names are read from content-members/ rather than from a list here, so
# a new member section is covered the day somebody adds it.
#
# The absence check is weaker than it looks against stale output: Hugo does not
# reliably delete files it no longer produces, so run this against trees built
# from a clean state, as CI does.
set -euo pipefail

cd "$(dirname "$0")"

status=0
shopt -s nullglob
sections=(content-members/*/*/)

if [ ${#sections[@]} -eq 0 ]; then
  echo "check-trees: no member sections found under content-members/" >&2
  exit 1
fi

for secdir in "${sections[@]}"; do
  path="/${secdir#content-members/}"

  if [ -e "public$path" ]; then
    echo "LEAK: public$path exists in the public tree" >&2
    status=1
  fi

  if [ ! -d "public-members$path" ]; then
    echo "MISSING: public-members$path was not built" >&2
    status=1
  fi

  if grep -rlF -- "$path" public >/dev/null 2>&1; then
    echo "LEAK: the public tree names $path in:" >&2
    grep -rlF -- "$path" public >&2
    status=1
  fi
done

if [ "$status" -eq 0 ]; then
  echo "check-trees: ${#sections[@]} member sections, none reachable from the public tree"
fi
exit "$status"

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

  # The section's own landing page is the address the sign-in button points at,
  # so the public tree names it on every page. That is not a leak: Caddy gates
  # the address and answers it with a redirect to Keycloak whether or not the
  # tree it is serving holds the page, so what is published is the door rather
  # than anything behind it. The pages below the landing page are the member
  # material itself and must stay unnamed here, which is what this matches: the
  # section path followed by a further segment.
  if grep -rlE -- "$path[a-z0-9]" public >/dev/null 2>&1; then
    echo "LEAK: the public tree names a page under $path in:" >&2
    grep -rlE -- "$path[a-z0-9]" public >&2
    status=1
  fi
done

# The mirror of the checks above: the public tree must not name a member path,
# and the member tree must not name the counting endpoint. The member section is
# not counted at all, because everyone past that gate is identified by name and
# a pageview count on the same machine as the gate's session log is a
# re-identification path the public side does not have.
#
# layouts/partials/analytics.html already refuses to render under three
# separate conditions. This asserts the outcome rather than trusting any of
# them, because the failure is silent: a counted member page looks exactly like
# an uncounted one. The hostname is spelled out here rather than read from the
# configuration, so the check keeps meaning the same thing if somebody empties
# the parameter or renames it.
analytics_host="analytics.prodeko.org"

if grep -rlF -- "$analytics_host" public-members >/dev/null 2>&1; then
  echo "LEAK: the member tree names $analytics_host in:" >&2
  grep -rlF -- "$analytics_host" public-members >&2
  status=1
fi

if [ "$status" -eq 0 ]; then
  echo "check-trees: ${#sections[@]} member sections, none reachable from the public tree"
  echo "check-trees: the member tree names no counting endpoint"
fi
exit "$status"

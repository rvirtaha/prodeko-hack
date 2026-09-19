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

  # The search index is gzip, so the grep above reads straight past it and
  # reports nothing whatever it contains. Decompress everything Pagefind wrote
  # into the public tree and run the same test against the contents. This is
  # what proves a member address is absent from the public index rather than
  # merely absent from the public HTML.
  if [ -d public/pagefind ] &&
     find public/pagefind \( -name '*.pf_index' -o -name '*.pf_fragment' -o -name '*.pf_meta' \) \
       -exec gzip -dc {} + 2>/dev/null | grep -qF -- "$path"; then
    echo "LEAK: the public search index contains $path" >&2
    status=1
  fi

  # Each member section carries its own index, inside the gate. Without this a
  # silently missing bundle degrades to "member search finds nothing" rather
  # than to a failed build.
  if [ ! -d "public-members$path/pagefind" ]; then
    echo "MISSING: public-members$path/pagefind was not built" >&2
    status=1
  fi
done

# A bundle at the root of the member tree would answer on /pagefind/, which is
# the address the public tree already serves. Pagefind's default output path
# puts it there, so this is one forgotten --output-path away.
if [ -d public-members/pagefind ]; then
  echo "LEAK: public-members/pagefind sits at the tree root, where /pagefind/ is public" >&2
  status=1
fi

# Pagefind aimed at the wrong tree is one mistyped path, and it would publish
# the member index to the world. The public index must describe exactly the
# pages the public build marked as indexable, no more.
if [ -d public/pagefind ]; then
  fragments=$(find public/pagefind/fragment -name '*.pf_fragment' | wc -l)
  indexable=$(grep -rlF 'data-pagefind-body' public --include='*.html' | wc -l)
  if [ "$fragments" -ne "$indexable" ]; then
    echo "MISMATCH: public index holds $fragments pages, the public tree marks $indexable" >&2
    status=1
  fi
fi

if [ "$status" -eq 0 ]; then
  echo "check-trees: ${#sections[@]} member sections, none reachable from the public tree"
fi
exit "$status"

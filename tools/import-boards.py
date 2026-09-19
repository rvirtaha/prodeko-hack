#!/usr/bin/env python3
"""Turn the live previous-boards page into one data file per year.

The page at prodeko.org/fi/guild/board/edelliset-hallitukset/ holds 58 years of
board members inside collapsible panels — a single HTML document of nearly half
a megabyte. It is the clearest case on the whole site for a year archive being
data rather than a hand-edited page: nobody is going to maintain 58 accordions
by hand, and every year the same edit has to be made in the same shape.

This is the parse docs/roadmap.md item E describes: one page, run once, to give
layouts/archive.html real data to render. The general converter from the Django
dump is a separate and larger job (item H).

    python3 tools/import-boards.py            # fetch and write site/data/boards/
    python3 tools/import-boards.py page.html  # parse a file already downloaded

Existing files are overwritten, so a rerun is safe.
"""

import html
import json
import re
import sys
import urllib.request
from pathlib import Path

SOURCE = "https://www.prodeko.org/fi/guild/board/edelliset-hallitukset/"
OUT_DIR = Path("site/data/boards")

# Each year is an anchor that toggles a panel, followed by that panel's members.
YEAR_SPLIT = re.compile(r'<a class="btn btn-primary[^"]*" data-toggle="collapse" href="#collapse-(\d{4})">')
MEMBER = re.compile(r'<div class="description">\s*<strong>(.*?)</strong>\s*<br\s*/?>\s*(.*?)\s*</div>', re.S)


def clean(fragment: str) -> str:
    """The source pads every name and role out to a fixed column width."""
    return html.unescape(re.sub(r"\s+", " ", re.sub("<[^>]+>", "", fragment))).strip().lstrip("﻿")


def parse(page: str) -> dict[str, list[dict[str, str]]]:
    parts = YEAR_SPLIT.split(page)
    years = {}
    for year, body in zip(parts[1::2], parts[2::2]):
        members = [
            {"name": clean(name), "role": clean(role)}
            for name, role in MEMBER.findall(body)
        ]
        members = [m for m in members if m["name"]]
        if members:
            years[year] = members
    return years


def as_yaml(year: str, members: list[dict[str, str]]) -> str:
    lines = [
        f"# Prodeko's {year} board, imported from {SOURCE}",
        "# by tools/import-boards.py. Edit here, not on the old site.",
        f'heading: "{year}"',
        f"sortKey: {year}",
        "members:",
    ]
    for member in members:
        # A name or role can carry a colon or a leading character YAML reads as
        # syntax, so both are quoted rather than guessed at.
        lines.append(f'  - name: "{member["name"]}"')
        lines.append(f'    role: "{member["role"]}"')
    return "\n".join(lines) + "\n"


def main() -> int:
    if len(sys.argv) > 1:
        page = Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace")
    else:
        print(f"fetching {SOURCE}")
        with urllib.request.urlopen(SOURCE, timeout=60) as response:
            page = response.read().decode("utf-8", errors="replace")

    years = parse(page)
    if not years:
        print("no boards found — the page's markup has changed", file=sys.stderr)
        return 1

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    for year, members in sorted(years.items()):
        (OUT_DIR / f"{year}.yaml").write_text(as_yaml(year, members), encoding="utf-8")

    total = sum(len(m) for m in years.values())
    print(f"wrote {len(years)} years ({min(years)}–{max(years)}), {total} people, to {OUT_DIR}/")
    return 0


if __name__ == "__main__":
    sys.exit(main())

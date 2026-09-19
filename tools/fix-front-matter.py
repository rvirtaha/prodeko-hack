#!/usr/bin/env python3
"""Quote YAML front-matter scalars that YAML would otherwise refuse.

A description written as

    description: Fuksin opas: haalarit, sitsit ja excut.

is not valid YAML — the second colon makes the line a nested mapping — and
Hugo fails the whole build over it, naming one file. Editors write lines like
that constantly, and so does anything generating content in bulk, so the fix
belongs in a script rather than in a person's memory.

Run from the repository root:

    python3 tools/fix-front-matter.py [--check]

--check reports what it would change and exits non-zero instead of writing,
which is the form to put in CI.
"""

import re
import sys
from pathlib import Path

CONTENT_ROOTS = [Path("site/content"), Path("site/content-members")]

# Keys whose values are free text an editor types. Structured keys (audiences,
# values, applySteps) are left alone: they are nested YAML, not scalars.
TEXT_KEYS = (
    "title",
    "description",
    "heroTitle",
    "heroKicker",
    "prospTitle",
    "prospKicker",
    "bandTitle",
    "bandSub",
    "valuesTitle",
    "valuesSub",
    "partnersTitle",
    "partnersLinkText",
    "ctaJoin",
    "ctaCompanies",
    "whatTitle",
    "applyTitle",
    "lifeTitle",
    "faqTitle",
    "captainsTitle",
    "captainsKicker",
)

KEY_RE = re.compile(r"^(\s*)(%s):[ \t]+(\S.*?)[ \t]*$" % "|".join(TEXT_KEYS))


def needs_quoting(value: str) -> bool:
    if value[0] in "\"'":
        return False
    # ": " and a trailing ":" both start a mapping; the rest open other YAML
    # constructs when they lead the value.
    return ": " in value or value.endswith(":") or value[0] in "[{&*!|>%@`#"


def quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


def process(path: Path, write: bool) -> list[str]:
    text = path.read_text(encoding="utf-8")
    if not text.startswith("---\n"):
        return []
    try:
        end = text.index("\n---", 4)
    except ValueError:
        return []

    changes, lines = [], []
    for line in text[4:end].split("\n"):
        match = KEY_RE.match(line)
        if match and needs_quoting(match.group(3)):
            line = f"{match.group(1)}{match.group(2)}: {quote(match.group(3))}"
            changes.append(f"{path}: {match.group(2)}")
        lines.append(line)

    if changes and write:
        path.write_text("---\n" + "\n".join(lines) + text[end:], encoding="utf-8")
    return changes


def main() -> int:
    check = "--check" in sys.argv
    changes = []
    for root in CONTENT_ROOTS:
        for path in sorted(root.rglob("*.md")):
            changes += process(path, write=not check)

    if not changes:
        print("front matter: nothing to quote")
        return 0

    verb = "would quote" if check else "quoted"
    print(f"front matter: {verb} {len(changes)} value(s)")
    for change in changes:
        print("  " + change)
    return 1 if check else 0


if __name__ == "__main__":
    sys.exit(main())

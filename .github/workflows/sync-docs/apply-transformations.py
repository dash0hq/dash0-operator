#!/usr/bin/env python3
# SPDX-FileCopyrightText: Copyright 2026 Dash0 Inc.
# SPDX-License-Identifier: Apache-2.0

"""Apply the transformations declared in transformations.yaml to a source markdown file.

This is used by the sync-docs-to-website workflow to adapt the operator's Helm chart docs into the website docs.
The transformations themselves live in transformations.yaml, which is the first-class source of truth for how the
docs are modified; this script only knows how to apply them.

Usage:
    apply-transformations.py <source.md> <transformations.yaml> <output.md>
"""

import datetime
import re
import sys

import yaml

# Mapping from the flag names allowed in transformations.yaml to Python's re flags.
_FLAG_NAMES = {
    "multiline": re.MULTILINE,
    "dotall": re.DOTALL,
    "ignorecase": re.IGNORECASE,
}


def main(argv):
    if len(argv) != 4:
        raise SystemExit(f"usage: {argv[0]} <source.md> <transformations.yaml> <output.md>")

    source_path, transformations_path, output_path = argv[1], argv[2], argv[3]

    with open(source_path, encoding="utf-8") as source_file:
        content = source_file.read()

    with open(transformations_path, encoding="utf-8") as transformations_file:
        transformations = yaml.safe_load(transformations_file)

    if not isinstance(transformations, list):
        raise SystemExit("error: transformations.yaml must contain a top-level list of transformations")

    placeholders = _build_placeholders()

    for index, transformation in enumerate(transformations, start=1):
        content = _apply(content, transformation, index, placeholders)
        print(f"applied: {_describe(transformation, index)}")

    with open(output_path, "w", encoding="utf-8") as output_file:
        output_file.write(content)

    print(f"wrote transformed document to {output_path}")


def _build_placeholders():
    """Build the placeholder values that can be referenced in the inserted/replacement text.

    The values are computed once per run so that every occurrence of a placeholder renders consistently. Currently the
    only supported placeholder is $timestamp, which renders the current UTC date/time, e.g. "2026-04-20T05:00:00.000Z".
    """
    now = datetime.datetime.now(datetime.timezone.utc)
    timestamp = now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{now.microsecond // 1000:03d}Z"
    return {"$timestamp": timestamp}


def _apply(content, transformation, index, placeholders):
    description = _describe(transformation, index)
    transformation_type = transformation.get("type")
    required = transformation.get("required", True)

    if transformation_type == "prepend":
        return _substitute_placeholders(transformation["content"], placeholders) + content

    if transformation_type == "replace-regex":
        pattern = re.compile(transformation["find"], _compile_flags(transformation))
        replace = _substitute_placeholders(transformation["replace"], placeholders)
        new_content, count = pattern.subn(replace, content)
        if count == 0 and required:
            raise SystemExit(f"error: replace-regex transformation matched nothing: {description}")
        return new_content

    if transformation_type == "remove-line":
        return _remove_line(content, transformation, description, required)

    raise SystemExit(f"error: unknown transformation type {transformation_type!r}: {description}")


def _describe(transformation, index):
    """Return a human-readable label for a transformation, used in log output and error messages."""
    return transformation.get("description", f"transformation #{index}")


def _compile_flags(transformation):
    flags = 0
    for name in transformation.get("flags", []):
        if name not in _FLAG_NAMES:
            raise ValueError(f"unknown regex flag {name!r}; allowed flags: {sorted(_FLAG_NAMES)}")
        flags |= _FLAG_NAMES[name]
    return flags


def _substitute_placeholders(text, placeholders):
    for placeholder, value in placeholders.items():
        text = text.replace(placeholder, value)
    return text


def _remove_line(content, transformation, description, required):
    """Remove the line matching the given literal.

    The `line` value is matched as a literal substring; the whole line(s) containing its first occurrence are removed.
    If the removal leaves multiple consecutive empty lines behind, they are normalized to a single empty line.
    """
    marker = transformation["line"]

    marker_index = content.find(marker)
    if marker_index == -1:
        if required:
            raise SystemExit(f"error: remove-line transformation did not find 'line' marker: {description}")
        return content

    # Expand the removal span to whole lines: back to the start of the line containing the marker and forward to the
    # end of the line where the marker ends (including its trailing newline, if any).
    line_start = content.rfind("\n", 0, marker_index) + 1
    line_end = content.find("\n", marker_index + len(marker))
    line_end = len(content) if line_end == -1 else line_end + 1

    new_content = content[:line_start] + content[line_end:]

    # Normalize runs of multiple empty lines (three or more consecutive newlines) down to a single empty line.
    new_content = re.sub(r"\n{3,}", "\n\n", new_content)
    return new_content


if __name__ == "__main__":
    main(sys.argv)

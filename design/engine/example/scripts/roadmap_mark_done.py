#!/usr/bin/env python3
import re
import sys

from common import emit, fail, ok, read_request, resolve_path, write_text_atomic


CHECKLIST_RE = re.compile(r"^(?P<prefix>\s*-\s\[)(?P<state>[ xX])(?P<suffix>\]\s+)(?P<title>.+)$")
TITLE_LABEL_RE = re.compile(r"^(?P<label>[0-9]+(?:\.[0-9]+)*)\s+(?P<summary>.+)$")


def main() -> int:
    try:
        request = read_request()
        file_value = request.get("file")
        match_value = request.get("match")
        if not isinstance(file_value, str) or not file_value:
            emit(fail("invalid_request", "field `file` must be a non-empty string"))
            return 0
        if not isinstance(match_value, dict):
            emit(fail("invalid_request", "field `match` must be an object"))
            return 0

        expected_label = match_value.get("label")
        expected_title = match_value.get("title")
        if not expected_label and not expected_title:
            emit(fail("invalid_request", "match requires `label` or `title`"))
            return 0

        path = resolve_path(file_value)
        lines = path.read_text(encoding="utf-8").splitlines()
        match_indexes: list[int] = []

        for index, line in enumerate(lines):
            checklist = CHECKLIST_RE.match(line)
            if not checklist:
                continue

            title = checklist.group("title")
            label = ""
            label_match = TITLE_LABEL_RE.match(title)
            if label_match:
                label = label_match.group("label")

            if expected_label and label == expected_label:
                match_indexes.append(index)
            elif expected_title and title == expected_title:
                match_indexes.append(index)

        if not match_indexes:
            emit(fail("not_found", "no matching roadmap item"))
            return 0
        if len(match_indexes) > 1:
            emit(fail("ambiguous_match", "multiple roadmap items matched"))
            return 0

        target = match_indexes[0]
        checklist = CHECKLIST_RE.match(lines[target])
        assert checklist is not None
        lines[target] = f"{checklist.group('prefix')}x{checklist.group('suffix')}{checklist.group('title')}"
        write_text_atomic(path, "\n".join(lines) + "\n")
        emit(
            ok(
                {
                    "lineNumber": target + 1,
                    "label": expected_label or "",
                    "title": checklist.group("title"),
                    "checked": True,
                }
            )
        )
        return 0
    except FileNotFoundError as exc:
        emit(fail("missing_file", str(exc)))
        return 0
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))
        return 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
import os
import pathlib
import re
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request


CHECKLIST_RE = re.compile(r"^(?P<prefix>\s*-\s\[)(?P<state>[ xX])(?P<suffix>\]\s+)(?P<title>.+)$")
TITLE_LABEL_RE = re.compile(r"^(?P<label>[0-9]+(?:\.[0-9]+)*)\s+(?P<summary>.+)$")


def write_text_atomic(path: pathlib.Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False, dir=str(path.parent)) as handle:
        handle.write(content)
        tmp_name = handle.name
    os.replace(tmp_name, path)


def main() -> int:
    try:
        request = read_request()
        config = request["config"]
        file_value = config["file"]
        match_value = config["match"]

        expected_label = match_value.get("label")
        expected_title = match_value.get("title")
        if not expected_label and not expected_title:
            emit(fail("plugin_error", "field `config.match` requires `label` or `title`"))
            return 0

        path = pathlib.Path(file_value)
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

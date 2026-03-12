#!/usr/bin/env python3
import os
import pathlib
import re
import sys
import tempfile

sys.dont_write_bytecode = True
sys.pycache_prefix = tempfile.mkdtemp(prefix="roadmap-mark-done-pycache-")
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
        hits: list[tuple[int, re.Match, str]] = []

        for index, line in enumerate(lines):
            m = CHECKLIST_RE.match(line)
            if not m:
                continue
            title = m.group("title")
            label_m = TITLE_LABEL_RE.match(title)
            label = label_m.group("label") if label_m else ""
            if (expected_label and label == expected_label) or (expected_title and title == expected_title):
                hits.append((index, m, label))

        if not hits:
            emit(fail("not_found", "no matching roadmap item"))
            return 0
        if len(hits) > 1:
            emit(fail("ambiguous_match", "multiple roadmap items matched"))
            return 0

        target, m, found_label = hits[0]
        lines[target] = f"{m.group('prefix')}x{m.group('suffix')}{m.group('title')}"
        write_text_atomic(path, "\n".join(lines) + "\n")
        emit(ok({"lineNumber": target + 1, "label": found_label, "title": m.group("title"), "checked": True}))
        return 0
    except FileNotFoundError as exc:
        emit(fail("missing_file", str(exc)))
        return 0
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))
        return 0


if __name__ == "__main__":
    sys.exit(main())

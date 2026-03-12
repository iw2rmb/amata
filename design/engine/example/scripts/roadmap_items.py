#!/usr/bin/env python3
import hashlib
import pathlib
import re
import sys
import tempfile

sys.dont_write_bytecode = True
sys.pycache_prefix = tempfile.mkdtemp(prefix="roadmap-items-pycache-")
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request


CHECKLIST_RE = re.compile(r"^\s*-\s\[(?P<state>[ xX])\]\s+(?P<title>.+)$")
META_RE = re.compile(r"^\s*-\s+(?P<key>[^:]+):\s*(?P<value>.*)$")
ACTION_RE = re.compile(r"^\s*\d+\.\s+(?P<value>.*)$")
TITLE_LABEL_RE = re.compile(r"^(?P<label>[0-9]+(?:\.[0-9]+)*)\s+(?P<summary>.+)$")

META_KEYS = {"Repository", "Component", "Verification", "Reasoning", "Scope", "Tests"}


def _parse_block(lines: list[str], start: int) -> tuple[dict, list[str], int, int]:
    """Parse the metadata block for a checklist item starting at `start`.

    Returns (meta, actions, end_index, next_i) where end_index is the last
    consumed line index and next_i is where the outer loop should resume.
    """
    meta = {k.lower(): "" for k in META_KEYS}
    actions: list[str] = []
    end = start
    j = start + 1
    while j < len(lines):
        if CHECKLIST_RE.match(lines[j]):
            break
        m = META_RE.match(lines[j])
        if m:
            key = m.group("key")
            if key in META_KEYS:
                meta[key.lower()] = m.group("value")
        else:
            m = ACTION_RE.match(lines[j])
            if m:
                actions.append(m.group("value"))
        end = j
        j += 1
    if not meta["verification"] and meta["tests"]:
        meta["verification"] = meta["tests"]
    return meta, actions, end, j


def parse_items(text: str) -> list[dict]:
    lines = text.splitlines()
    items: list[dict] = []
    i = 0
    while i < len(lines):
        m = CHECKLIST_RE.match(lines[i])
        if not m:
            i += 1
            continue

        title = m.group("title")
        checked = m.group("state") != " "
        label, summary = "", title
        lm = TITLE_LABEL_RE.match(title)
        if lm:
            label, summary = lm.group("label"), lm.group("summary")

        meta, actions, end_index, next_i = _parse_block(lines, i)
        items.append(
            {
                "checked": checked,
                "title": title,
                "label": label,
                "summary": summary,
                "lineNumber": i + 1,
                "endLine": end_index + 1,
                **meta,
                "reasoning": meta["reasoning"] or "high",
                "reasoningSource": "roadmap" if meta["reasoning"] else "default",
                "actions": actions,
                "block": "\n".join(lines[i : end_index + 1]),
                "handle": {
                    "kind": "roadmap.label",
                    "label": label,
                    "titleHash": hashlib.sha256(title.encode()).hexdigest(),
                    "lineNumber": i + 1,
                },
            }
        )
        i = next_i
    return items


def main() -> int:
    try:
        request = read_request()
        path = pathlib.Path(request["config"]["file"])
        text = path.read_text(encoding="utf-8")
        emit(ok({"items": parse_items(text)}))
        return 0
    except FileNotFoundError as exc:
        emit(fail("missing_file", str(exc)))
        return 0
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))
        return 0


if __name__ == "__main__":
    sys.exit(main())

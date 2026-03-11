#!/usr/bin/env python3
import hashlib
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request


CHECKLIST_RE = re.compile(r"^\s*-\s\[(?P<state>[ xX])\]\s+(?P<title>.+)$")
META_RE = re.compile(r"^\s*-\s+(?P<key>[^:]+):\s*(?P<value>.*)$")
ACTION_RE = re.compile(r"^\s*\d+\.\s+(?P<value>.*)$")
TITLE_LABEL_RE = re.compile(r"^(?P<label>[0-9]+(?:\.[0-9]+)*)\s+(?P<summary>.+)$")


def parse_items(text: str) -> list[dict]:
    lines = text.splitlines()
    items: list[dict] = []
    i = 0
    while i < len(lines):
        line = lines[i]
        match = CHECKLIST_RE.match(line)
        if not match:
            i += 1
            continue

        title = match.group("title")
        checked = match.group("state") != " "
        label = ""
        summary = title
        label_match = TITLE_LABEL_RE.match(title)
        if label_match:
            label = label_match.group("label")
            summary = label_match.group("summary")

        repository = ""
        component = ""
        verification = ""
        reasoning = ""
        scope = ""
        tests = ""
        actions: list[str] = []
        block_lines = [line]
        end_index = i
        j = i + 1

        while j < len(lines):
            next_line = lines[j]
            if CHECKLIST_RE.match(next_line):
                break

            block_lines.append(next_line)
            meta_match = META_RE.match(next_line)
            if meta_match:
                key = meta_match.group("key")
                value = meta_match.group("value")
                if key == "Repository":
                    repository = value
                elif key == "Component":
                    component = value
                elif key == "Verification":
                    verification = value
                elif key == "Reasoning":
                    reasoning = value
                elif key == "Scope":
                    scope = value
                elif key == "Tests":
                    tests = value
                    if not verification:
                        verification = value
            else:
                action_match = ACTION_RE.match(next_line)
                if action_match:
                    actions.append(action_match.group("value"))

            end_index = j
            j += 1

        block_text = "\n".join(block_lines)
        items.append(
            {
                "checked": checked,
                "title": title,
                "label": label,
                "summary": summary,
                "lineNumber": i + 1,
                "endLine": end_index + 1,
                "repository": repository,
                "component": component,
                "verification": verification,
                "reasoning": reasoning or "high",
                "reasoningSource": "roadmap" if reasoning else "default",
                "scope": scope,
                "tests": tests,
                "actions": actions,
                "block": block_text,
                "handle": {
                    "kind": "roadmap.label",
                    "label": label,
                    "titleHash": hashlib.sha256(title.encode("utf-8")).hexdigest(),
                    "lineNumber": i + 1,
                },
            }
        )

        i = j
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

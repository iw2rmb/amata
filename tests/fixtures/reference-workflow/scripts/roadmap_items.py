#!/usr/bin/env python3
import argparse
import hashlib
import json
import pathlib
import re
import sys
from typing import Any


CHECKLIST_RE = re.compile(r"^\s*-\s\[(?P<state>[ xX])\]\s+(?P<title>.+)$")
META_RE = re.compile(r"^\s*-\s+(?P<key>[^:]+):\s*(?P<value>.*)$")
ACTION_RE = re.compile(r"^\s*\d+\.\s+(?P<value>.*)$")
TITLE_LABEL_RE = re.compile(r"^(?P<label>[0-9]+(?:\.[0-9]+)*)\s+(?P<summary>.+)$")

META_KEYS = {"Repository", "Component", "Verification", "Reasoning", "Scope", "Tests"}


def _parse_block(lines: list[str], start: int) -> tuple[dict[str, str], list[str], int, int]:
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


def parse_items(text: str) -> list[dict[str, Any]]:
    lines = text.splitlines()
    items: list[dict[str, Any]] = []
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


def parse_carry(entries: list[str]) -> dict[str, str]:
    carry: dict[str, str] = {}
    for entry in entries:
        key, sep, value = entry.partition("=")
        if sep == "" or key == "":
            raise ValueError(f"invalid --carry value {entry!r}; expected key=value")
        carry[key] = value
    return carry


def find_matches(items: list[dict[str, Any]], label: str, title: str) -> list[dict[str, Any]]:
    matches: list[dict[str, Any]] = []
    for item in items:
        if label and item["label"] == label:
            matches.append(item)
            continue
        if title and item["title"] == title:
            matches.append(item)
    return matches


def emit(value: Any) -> int:
    json.dump(value, sys.stdout, indent=2, sort_keys=True)
    sys.stdout.write("\n")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)

    def add_common_arguments(command: argparse.ArgumentParser, include_carry: bool = False) -> None:
        command.add_argument("--file", required=True)
        if include_carry:
            command.add_argument("--carry", action="append", default=[])

    list_cmd = subparsers.add_parser("list")
    add_common_arguments(list_cmd)

    next_open_cmd = subparsers.add_parser("next-open")
    add_common_arguments(next_open_cmd)

    match_cmd = subparsers.add_parser("match")
    add_common_arguments(match_cmd, include_carry=True)
    match_cmd.add_argument("--label", default="")
    match_cmd.add_argument("--title", default="")

    summary_cmd = subparsers.add_parser("summary")
    add_common_arguments(summary_cmd)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()

    if args.command == "match" and not args.label and not args.title:
        parser.error("match requires --label or --title")

    try:
        path = pathlib.Path(args.file).expanduser()
        items = parse_items(path.read_text(encoding="utf-8"))
        carry = parse_carry(getattr(args, "carry", []))
    except FileNotFoundError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    except Exception as exc:
        print(str(exc), file=sys.stderr)
        return 1

    if args.command == "list":
        return emit({"items": items})

    if args.command == "next-open":
        open_items = [item for item in items if not item["checked"]]
        return emit(
            {
                "hasOpenItem": len(open_items) > 0,
                "openCount": len(open_items),
                "item": open_items[0] if open_items else None,
            }
        )

    if args.command == "match":
        matches = find_matches(items, args.label, args.title)
        if len(matches) > 1:
            print("multiple roadmap items matched", file=sys.stderr)
            return 1
        return emit(
            {
                "found": len(matches) == 1,
                "item": matches[0] if matches else None,
                "carry": carry,
            }
        )

    if args.command == "summary":
        open_count = sum(0 if item["checked"] else 1 for item in items)
        return emit({"hasOpenItems": open_count > 0, "openCount": open_count})

    parser.error(f"unsupported command {args.command!r}")
    return 2


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
import json
import os
import pathlib
import tempfile
from typing import Any


def read_request() -> dict[str, Any]:
    data = json.load(os.fdopen(0))
    if not isinstance(data, dict):
        raise ValueError("request must be a JSON object")
    return data


def step_result(status: str, value: Any = None, error: dict[str, str] | None = None, artifacts: dict[str, Any] | None = None) -> dict[str, Any]:
    return {
        "status": status,
        "value": value,
        "error": error,
        "artifacts": artifacts or {"stdout": None, "stderr": None, "files": {}},
    }


def ok(value: Any = None, artifacts: dict[str, Any] | None = None) -> dict[str, Any]:
    return step_result("succeeded", value=value, artifacts=artifacts)


def fail(code: str, message: str, artifacts: dict[str, Any] | None = None) -> dict[str, Any]:
    return step_result("failed", value=None, error={"code": code, "message": message}, artifacts=artifacts)


def emit(result: dict[str, Any]) -> None:
    print(json.dumps(result, indent=2, sort_keys=True))


def resolve_path(path_text: str) -> pathlib.Path:
    path = pathlib.Path(path_text)
    if not path.is_absolute():
        path = pathlib.Path.cwd() / path
    return path


def write_text_atomic(path: pathlib.Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False, dir=str(path.parent)) as handle:
      handle.write(content)
      tmp_name = handle.name
    os.replace(tmp_name, path)

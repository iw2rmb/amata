#!/usr/bin/env python3
import pathlib
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        check=False,
        capture_output=True,
        text=True,
    )


def changed_paths() -> list[str]:
    result = run_git("status", "--porcelain")
    if result.returncode != 0:
        raise RuntimeError(result.stderr.strip() or "git status failed")

    paths: list[str] = []
    for raw_line in result.stdout.splitlines():
        if len(raw_line) < 4:
            continue
        path = raw_line[3:]
        if " -> " in path:
            path = path.split(" -> ", 1)[1]
        paths.append(path)
    return paths


def is_excluded(path: str, excluded: list[str]) -> bool:
    return any(path == prefix or path.startswith(prefix.rstrip("/") + "/") for prefix in excluded)


def main() -> int:
    try:
        request = read_request()
        config = request.get("config")
        if not isinstance(config, dict):
            emit(fail("invalid_request", "field `config` must be an object"))
            return 0

        message = config.get("message")
        excluded = config.get("exclude_paths", [])
        if not isinstance(message, str) or not message:
            emit(fail("invalid_request", "field `config.message` must be a non-empty string"))
            return 0
        if not isinstance(excluded, list) or any(not isinstance(item, str) for item in excluded):
            emit(fail("invalid_request", "field `config.exclude_paths` must be an array of strings"))
            return 0

        inside_repo = run_git("rev-parse", "--is-inside-work-tree")
        if inside_repo.returncode != 0 or inside_repo.stdout.strip() != "true":
            emit(fail("git_error", "current working directory is not a git repository"))
            return 0

        included = [path for path in changed_paths() if not is_excluded(path, excluded)]
        if not included:
            emit(ok({"committed": False, "commit": None, "paths": []}))
            return 0

        add_result = run_git("add", "-A", "--", *included)
        if add_result.returncode != 0:
            emit(fail("git_error", add_result.stderr.strip() or "git add failed"))
            return 0

        staged_check = run_git("diff", "--cached", "--quiet", "--")
        if staged_check.returncode == 0:
            emit(ok({"committed": False, "commit": None, "paths": included}))
            return 0

        commit_result = run_git("commit", "-m", message)
        if commit_result.returncode != 0:
            emit(fail("git_error", commit_result.stderr.strip() or commit_result.stdout.strip() or "git commit failed"))
            return 0

        sha_result = run_git("rev-parse", "HEAD")
        sha = sha_result.stdout.strip() if sha_result.returncode == 0 else None
        emit(ok({"committed": True, "commit": sha, "paths": included}))
        return 0
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))
        return 0


if __name__ == "__main__":
    sys.exit(main())

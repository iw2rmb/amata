#!/usr/bin/env python3
import pathlib
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
sys.pycache_prefix = tempfile.mkdtemp(prefix="git-commit-pycache-")
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request


class GitError(RuntimeError):
    pass


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
        raise GitError(result.stderr.strip() or "git status failed")

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


def main() -> None:
    try:
        request = read_request()
        config = request["config"]
        message = config["message"]
        excluded = config.get("exclude_paths", [])

        inside_repo = run_git("rev-parse", "--is-inside-work-tree")
        if inside_repo.returncode != 0 or inside_repo.stdout.strip() != "true":
            raise GitError("current working directory is not a git repository")

        included = [path for path in changed_paths() if not is_excluded(path, excluded)]
        if not included:
            emit(ok({"committed": False, "commit": None, "paths": []}))
            return

        add_result = run_git("add", "-A", "--", *included)
        if add_result.returncode != 0:
            raise GitError(add_result.stderr.strip() or "git add failed")

        staged_check = run_git("diff", "--cached", "--quiet", "--")
        if staged_check.returncode == 0:
            emit(ok({"committed": False, "commit": None, "paths": included}))
            return

        commit_result = run_git("commit", "-m", message)
        if commit_result.returncode != 0:
            raise GitError(commit_result.stderr.strip() or commit_result.stdout.strip() or "git commit failed")

        sha_result = run_git("rev-parse", "HEAD")
        sha = sha_result.stdout.strip() if sha_result.returncode == 0 else None
        emit(ok({"committed": True, "commit": sha, "paths": included}))
    except GitError as exc:
        emit(fail("git_error", str(exc)))
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))


if __name__ == "__main__":
    main()

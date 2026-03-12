#!/usr/bin/env python3
import pathlib
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True
sys.pycache_prefix = tempfile.mkdtemp(prefix="git-commit-pycache-")
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request
from scripts.git_common import GitError, filter_excluded, head_sha, inspect_repo, run_git


def main() -> None:
    try:
        request = read_request()
        config = request["config"]
        message = config["message"]
        excluded = config.get("exclude_paths", [])

        snapshot = inspect_repo(include_untracked=True)
        if not snapshot.is_repo:
            raise GitError("current working directory is not a git repository")

        included = filter_excluded(snapshot.files, excluded)
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

        sha = head_sha()
        emit(ok({"committed": True, "commit": sha, "paths": included}))
    except GitError as exc:
        emit(fail("git_error", str(exc)))
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))


if __name__ == "__main__":
    main()

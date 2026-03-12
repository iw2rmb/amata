#!/usr/bin/env python3
import pathlib
import sys
import tempfile

sys.dont_write_bytecode = True
sys.pycache_prefix = tempfile.mkdtemp(prefix="git-inspect-pycache-")
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

from sdk.python import emit, fail, ok, read_request
from scripts.git_common import GitError, inspect_repo


def main() -> None:
    try:
        request = read_request()
        config = request.get("config") or {}
        include_untracked = config.get("include_untracked", True)

        if not isinstance(include_untracked, bool):
            raise ValueError("config.include_untracked must be boolean")

        snapshot = inspect_repo(include_untracked=include_untracked)
        emit(ok({
            "isRepo": snapshot.is_repo,
            "hasDiff": snapshot.has_diff,
            "files": snapshot.files,
        }))
    except GitError as exc:
        emit(fail("git_error", str(exc)))
    except Exception as exc:
        emit(fail("plugin_error", str(exc)))


if __name__ == "__main__":
    main()

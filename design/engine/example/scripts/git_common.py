from __future__ import annotations

from dataclasses import dataclass
import subprocess


class GitError(RuntimeError):
    pass


@dataclass(frozen=True)
class RepoSnapshot:
    is_repo: bool
    has_diff: bool
    files: list[str]


def run_git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        check=False,
        capture_output=True,
        text=True,
    )


def parse_status_z(data: bytes) -> list[str]:
    parts = data.split(b"\0")
    files: list[str] = []
    i = 0

    while i < len(parts):
        entry = parts[i]
        if not entry:
            i += 1
            continue
        if len(entry) < 3:
            i += 1
            continue

        status = entry[:2].decode("ascii", "replace")
        path = entry[3:]
        i += 1

        if ("R" in status or "C" in status) and i < len(parts) and parts[i]:
            path = parts[i]
            i += 1

        files.append(path.decode("utf-8", "surrogateescape"))

    return sorted(dict.fromkeys(files))


def inspect_repo(*, include_untracked: bool = True) -> RepoSnapshot:
    inside_repo = run_git("rev-parse", "--is-inside-work-tree")
    if inside_repo.returncode != 0 or inside_repo.stdout.strip() != "true":
        return RepoSnapshot(is_repo=False, has_diff=False, files=[])

    mode = "all" if include_untracked else "no"
    result = subprocess.run(
        ["git", "status", "--porcelain=v1", "-z", f"--untracked-files={mode}"],
        check=False,
        capture_output=True,
    )
    if result.returncode != 0:
        message = result.stderr.decode("utf-8", "replace").strip() or "git status failed"
        raise GitError(message)

    files = parse_status_z(result.stdout)
    return RepoSnapshot(is_repo=True, has_diff=bool(files), files=files)


def is_excluded(path: str, excluded: list[str]) -> bool:
    return any(path == prefix or path.startswith(prefix.rstrip("/") + "/") for prefix in excluded)


def filter_excluded(paths: list[str], excluded: list[str]) -> list[str]:
    return [path for path in paths if not is_excluded(path, excluded)]


def head_sha() -> str | None:
    result = run_git("rev-parse", "HEAD")
    if result.returncode != 0:
        return None
    return result.stdout.strip()

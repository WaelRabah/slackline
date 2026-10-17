#!/usr/bin/env python3
"""Validate a release input and publish already verified release packages."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


class ReleaseError(Exception):
    pass


def command(*args, optional=False):
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode and not optional:
        raise ReleaseError(f"{args[0]} {args[1]} failed; no existing tag or published asset will be overwritten")
    return result


def version_tag(value):
    version = value.strip().removeprefix("v")
    number = r"(?:0|[1-9][0-9]*)"
    identifier = r"(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
    if not re.fullmatch(rf"{number}\.{number}\.{number}(?:-{identifier}(?:\.{identifier})*)?", version, flags=re.ASCII):
        raise ReleaseError("Enter a version such as 0.3.0, v0.3.0, or 0.3.0-rc.1 (no build metadata)")
    return "v" + version


def repository():
    repo = os.environ.get("GITHUB_REPOSITORY", "")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo):
        raise ReleaseError("GITHUB_REPOSITORY must identify the release repository")
    return repo


def release_info(repo, tag):
    result = command("gh", "api", "--include", f"repos/{repo}/releases/tags/{tag}", optional=True)
    status = re.match(r"HTTP/\S+ (\d{3})\b", result.stdout)
    if status and status[1] == "404":
        return None
    if result.returncode or not status or status[1] != "200":
        raise ReleaseError("Could not verify whether the GitHub release already exists")
    try:
        value = json.loads(result.stdout.split("\n\n", 1)[1])
        if value["tag_name"] != tag or not isinstance(value["draft"], bool):
            raise ValueError()
        return value
    except (IndexError, KeyError, ValueError):
        raise ReleaseError("GitHub returned invalid release metadata") from None


def remote_tag(tag):
    result = command("git", "ls-remote", "--exit-code", "--tags", "origin", f"refs/tags/{tag}", optional=True)
    if result.returncode not in (0, 2):
        raise ReleaseError("Could not check the remote tag; publication has not started")
    return result.returncode == 0


def source_sha():
    sha = os.environ.get("SOURCE_SHA", os.environ.get("GITHUB_SHA", ""))
    if not re.fullmatch(r"[0-9a-f]{40}", sha) or command("git", "rev-parse", "HEAD").stdout.strip() != sha:
        raise ReleaseError("The checkout must match the exact source commit selected for release")
    return sha


def prepare(value, dry_run):
    tag = version_tag(value)
    if os.environ.get("GITHUB_REF") != "refs/heads/main" and not dry_run:
        raise ReleaseError("Published releases must be started from main; other branches allow dry runs only")
    sha = source_sha()
    repo = repository()
    if remote_tag(tag) or release_info(repo, tag) is not None:
        raise ReleaseError(f"{tag} already exists; choose an unused version")
    values = {"tag": tag, "version": tag[1:], "sha": sha}
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        for key, value in values.items():
            output.write(f"{key}={value}\n")
    print(f"Validated {tag} at {sha}; {'package verification only' if dry_run else 'publish after package verification'}")


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def verify_bundle(directory, tag):
    version = tag[1:]
    packages = {f"slackline_{version}_{system}_{arch}.{extension}"
                for system, extensions in [("darwin", ("tar.gz", "pkg")), ("linux", ("tar.gz", "deb", "rpm"))]
                for arch in ("amd64", "arm64") for extension in extensions}
    expected = packages | {"install.sh"}
    try:
        files = {path.name for path in directory.iterdir()}
        if files != expected | {"checksums.txt"}:
            raise ReleaseError("The release bundle must contain every installer, install.sh, and checksums.txt for the requested version")
        if any((directory / name).is_symlink() or not (directory / name).is_file() for name in files):
            raise ValueError()
        checksums = {}
        for line in (directory / "checksums.txt").read_text().splitlines():
            checksum, name = line.split()
            if name in checksums or not re.fullmatch(r"[0-9a-f]{64}", checksum):
                raise ValueError()
            checksums[name] = checksum
        if set(checksums) != expected:
            raise ValueError()
        for name, checksum in checksums.items():
            path = directory / name
            if path.is_symlink() or not path.is_file() or digest(path) != checksum:
                raise ValueError()
    except (OSError, ValueError):
        raise ReleaseError("Release checksums are invalid or a required package is corrupt") from None
    return sorted(expected | {"checksums.txt"})


def publish(value, directory):
    if os.environ.get("GITHUB_REF") != "refs/heads/main" or os.environ.get("GITHUB_EVENT_NAME") != "workflow_dispatch":
        raise ReleaseError("Publication requires a version-input workflow run on main")
    tag = version_tag(value)
    sha = source_sha()
    assets = verify_bundle(directory, tag)
    repo = repository()
    release = release_info(repo, tag)
    exists = remote_tag(tag)
    if exists:
        command("git", "fetch", "--no-tags", "origin", f"refs/tags/{tag}")
        if command("git", "rev-parse", "FETCH_HEAD^{commit}").stdout.strip() != sha:
            raise ReleaseError("The existing tag points to another commit; it will not be moved")
    elif release is not None:
        raise ReleaseError("A release already uses this version without a matching tag")
    else:
        command("git", "tag", tag, sha)
        command("git", "push", "origin", f"refs/tags/{tag}")
    paths = [str(directory / name) for name in assets]
    if release is not None and not release["draft"]:
        # A rerun after publication may retry the public installer check, but
        # must verify immutable downloads rather than overwrite a release.
        with tempfile.TemporaryDirectory(prefix="slackline-release-verify-") as temporary:
            command("gh", "release", "download", tag, "--repo", repo, "--dir", temporary)
            remote = Path(temporary)
            verify_bundle(remote, tag)
            if any(digest(remote / name) != digest(directory / name) for name in assets):
                raise ReleaseError("Published assets differ from this build; choose a new version")
    else:
        if release is None:
            args = ["gh", "release", "create", tag, "--repo", repo, "--verify-tag", "--generate-notes", "--draft"]
            if "-" in tag:
                args.append("--prerelease")
            command(*args, *paths)
        else:
            command("gh", "release", "upload", tag, "--repo", repo, "--clobber", *paths)
        command("gh", "release", "edit", tag, "--repo", repo, "--draft=false")
    print(f"Published https://github.com/{repo}/releases/tag/{tag}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="mode", required=True)
    preflight = sub.add_parser("prepare")
    preflight.add_argument("--dry-run", action="store_true")
    verification = sub.add_parser("verify")
    verification.add_argument("--directory", type=Path, default=Path("dist"))
    publication = sub.add_parser("publish")
    publication.add_argument("--directory", type=Path, default=Path("dist"))
    args = parser.parse_args()
    if args.mode == "prepare":
        prepare(os.environ.get("RELEASE_VERSION", ""), args.dry_run)
    elif args.mode == "verify":
        verify_bundle(args.directory, version_tag(os.environ.get("RELEASE_VERSION", "")))
        print("Every release package and installer has a valid checksum")
    else:
        publish(os.environ.get("RELEASE_VERSION", ""), args.directory)


if __name__ == "__main__":
    try:
        main()
    except ReleaseError as error:
        sys.exit(f"Release: {error}")

"""Exercise release ordering against a temporary Git remote and a fake GitHub API."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("release", Path(__file__).resolve().parents[1] / "scripts/release.py")
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)

GH = r'''#!/usr/bin/env python3
import json, os, pathlib, shutil, subprocess, sys
args = sys.argv[1:]
root = pathlib.Path(os.environ['FAKE_ROOT'])
state = json.loads((root / 'github.json').read_text())
with (root / 'commands.jsonl').open('a') as log: log.write(json.dumps(args)+'\n')
if args[0] == 'api':
    assert '--paginate' in args and '--slurp' in args
    assert '/releases?per_page=100' in args[-1]
    if state.get('api_status'):
        print('API unavailable', file=sys.stderr); sys.exit(1)
    values = [state['release']] if state['release'] else []
    if os.environ.get('FAKE_READ_ONLY') == '1': values = [r for r in values if not r['draft']]
    print(json.dumps([values]))
    sys.exit(0)
if args[:2] == ['release', 'create']:
    if state.get('create_fail'): sys.exit(1)
    tag = args[2]
    assert subprocess.run(['git','--git-dir',str(root/'remote.git'),'show-ref','--verify','refs/tags/'+tag],capture_output=True).returncode == 0
    assert '--draft' in args and '--verify-tag' in args
    assets = [pathlib.Path(a) for a in args if pathlib.Path(a).is_file()]
    assert len(assets) == 12
    published = root/'published'
    published.mkdir(exist_ok=True)
    for path in assets: shutil.copyfile(path,published/path.name)
    state['release'] = {'tag_name': tag, 'draft': True}
    state['prerelease'] = '--prerelease' in args
elif args[:2] == ['release', 'upload']:
    if state.get('upload_fail'): sys.exit(1)
    assert state['release']['draft']
    published = root/'published'
    published.mkdir(exist_ok=True)
    for value in args:
        path=pathlib.Path(value)
        if path.is_file(): shutil.copyfile(path,published/path.name)
elif args[:2] == ['release', 'edit']:
    state['release']['draft'] = False
elif args[:2] == ['release', 'download']:
    dest = pathlib.Path(args[args.index('--dir')+1])
    for path in (root/'published').iterdir(): shutil.copyfile(path, dest/path.name)
else: sys.exit(1)
(root/'github.json').write_text(json.dumps(state))
'''


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "source"
        self.repo.mkdir()
        self.remote = self.root / "remote.git"
        self.tools = self.root / "tools"
        self.tools.mkdir()
        (self.tools / "gh").write_text(GH)
        (self.tools / "gh").chmod(0o755)
        self.bundle = self.root / "bundle"
        self.bundle.mkdir()
        self.state(release=None)
        self.git("init", "--bare", str(self.remote))
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release fixture")
        self.git("config", "user.email", "fixture@example.test")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "tag.gpgsign", "false")
        self.git("commit", "--allow-empty", "-m", "fixture")
        self.git("remote", "add", "origin", str(self.remote))
        self.git("push", "-u", "origin", "main")
        self.sha = self.git("rev-parse", "HEAD")
        self.env = dict(os.environ, PATH=str(self.tools)+os.pathsep+os.environ['PATH'],
                        GH_TOKEN="fixture-only", GITHUB_TOKEN="fixture-only", GH_DEBUG="",
                        FAKE_ROOT=str(self.root), GITHUB_REPOSITORY="fixture/slackline",
                        GITHUB_SHA=self.sha, SOURCE_SHA=self.sha, GITHUB_REF="refs/heads/main",
                        GITHUB_EVENT_NAME="workflow_dispatch", GITHUB_OUTPUT=str(self.root / "outputs"))
        self.make_bundle("0.3.0")
        old = Path.cwd()
        os.chdir(self.repo)
        self.addCleanup(os.chdir, old)
        self.environment = patch.dict(os.environ, self.env, clear=True)
        self.environment.start()
        self.addCleanup(self.environment.stop)

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, stderr=subprocess.PIPE, text=True).strip()

    def state(self, **values):
        path = self.root / "github.json"
        state = json.loads(path.read_text()) if path.exists() else {}
        state.update(values)
        path.write_text(json.dumps(state))
        return state

    def commands(self):
        path = self.root / "commands.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def make_bundle(self, version):
        for file in self.bundle.iterdir(): file.unlink()
        names = [f"slackline_{version}_{system}_{arch}.{extension}"
                 for system, extensions in [("darwin", ("tar.gz", "pkg")), ("linux", ("tar.gz", "deb", "rpm"))]
                 for arch in ("amd64", "arm64") for extension in extensions] + ["install.sh"]
        checksums = []
        for name in names:
            data = ("fixture bytes: " + name).encode()
            (self.bundle / name).write_bytes(data)
            checksums.append(hashlib.sha256(data).hexdigest()+"  "+name)
        (self.bundle / "checksums.txt").write_text("\n".join(checksums)+"\n")

    def test_version_input_is_strict_and_normalizes_prefix(self):
        for value, expected in [("0.3.0", "v0.3.0"), (" v1.2.3-rc.1 ", "v1.2.3-rc.1")]:
            self.assertEqual(release.version_tag(value), expected)
        for value in ["", "latest", "1.2", "01.2.3", "1.2.3-01", "1.2.3-rc..1", "1.2.3+build", "1.2.3\nsha=evil", "1.2.3; touch evil"]:
            with self.subTest(value=value), self.assertRaises(release.ReleaseError): release.version_tag(value)
        self.assertFalse((self.repo / "evil").exists())

    def test_preflight_freezes_source_without_creating_a_tag(self):
        release.prepare("0.3.0", False)
        outputs = (self.root / "outputs").read_text()
        self.assertIn("tag=v0.3.0\n", outputs)
        self.assertIn("sha="+self.sha, outputs)
        self.assertEqual(self.git("ls-remote", "--tags", "origin"), "")
        self.assertTrue(all(args[0] == "api" for args in self.commands()))

    def test_nonmain_only_allows_dry_run(self):
        os.environ["GITHUB_REF"] = "refs/heads/topic"
        with self.assertRaises(release.ReleaseError): release.prepare("0.3.0", False)
        release.prepare("0.3.0", True)
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        self.assertEqual(self.git("ls-remote", "--tags", "origin"), "")

    def test_existing_version_and_api_failures_are_rejected(self):
        self.git("tag", "v0.3.0")
        self.git("push", "origin", "v0.3.0")
        with self.assertRaises(release.ReleaseError): release.prepare("0.3.0", False)
        self.state(release={"tag_name": "v0.4.0", "draft": True})
        with self.assertRaises(release.ReleaseError): release.prepare("0.4.0", False)
        self.state(release=None, api_status=503)
        with self.assertRaises(release.ReleaseError): release.prepare("0.5.0", False)
        self.assertFalse((self.root / "outputs").exists())

    def test_paginated_draft_lookup_and_ambiguous_metadata(self):
        data = [[{"tag_name": "v0.2.0", "draft": False}], [{"tag_name": "v0.3.0", "draft": True}]]
        result = subprocess.CompletedProcess([], 0, json.dumps(data), "")
        with patch.object(release, "command", return_value=result):
            self.assertTrue(release.release_info("fixture/slackline", "v0.3.0")["draft"])
        data[0].append({"tag_name": "v0.3.0", "draft": False})
        result.stdout = json.dumps(data)
        with patch.object(release, "command", return_value=result), self.assertRaises(release.ReleaseError):
            release.release_info("fixture/slackline", "v0.3.0")

    def test_hidden_draft_is_rechecked_by_publisher_before_tag_creation(self):
        self.state(release={"tag_name": "v0.3.0", "draft": True})
        os.environ["FAKE_READ_ONLY"] = "1"
        release.prepare("0.3.0", False)
        os.environ["FAKE_READ_ONLY"] = "0"
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        self.assertEqual(self.git("ls-remote", "--tags", "origin"), "")
        self.assertTrue(all(args[0] == "api" for args in self.commands()))

    def test_command_errors_keep_diagnostics_but_redact_tokens_and_urls(self):
        failed = subprocess.CompletedProcess(["gh", "release"], 1, "", "HTTP 403 fixture-only https://example.test/?signature=private\n")
        with patch.object(release.subprocess, "run", return_value=failed), self.assertRaises(release.ReleaseError) as caught:
            release.command("gh", "release")
        message = str(caught.exception)
        self.assertIn("HTTP 403", message)
        self.assertNotIn("fixture-only", message)
        self.assertNotIn("signature=private", message)
        self.assertNotIn("example.test", message)

    def test_tag_lookup_failure_stops_before_build(self):
        self.git("remote", "set-url", "origin", str(self.root / "missing.git"))
        with self.assertRaises(release.ReleaseError): release.prepare("0.3.0", False)
        self.assertEqual(self.commands(), [])

    def test_corrupt_or_incomplete_packages_prevent_tag_creation(self):
        (self.bundle / "install.sh").write_text("corrupt")
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        self.assertEqual(self.git("ls-remote", "--tags", "origin"), "")
        self.assertEqual(self.commands(), [])
        self.make_bundle("0.3.0")
        (self.bundle / "slackline_0.3.0_darwin_arm64.pkg").unlink()
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        self.assertEqual(self.git("ls-remote", "--tags", "origin"), "")

    def test_publish_creates_tag_then_draft_with_all_assets_then_publishes(self):
        release.publish("0.3.0", self.bundle)
        self.assertEqual(self.git("ls-remote", "--tags", "origin", "refs/tags/v0.3.0").split()[0], self.sha)
        self.assertFalse(self.state()["release"]["draft"])
        mutation = [args[:2] for args in self.commands() if args[0] == "release"]
        self.assertEqual(mutation, [["release", "create"], ["release", "edit"]])

    def test_published_rerun_verifies_assets_without_overwriting(self):
        release.publish("0.3.0", self.bundle)
        before = len(self.commands())
        release.publish("0.3.0", self.bundle)
        mutation = [args[:2] for args in self.commands()[before:] if args[0] == "release"]
        self.assertEqual(mutation, [["release", "download"]])

    def test_published_release_cannot_be_replaced_by_a_different_build(self):
        release.publish("0.3.0", self.bundle)
        (self.bundle / "install.sh").write_text("a different build")
        manifest = self.bundle / "checksums.txt"
        lines = [release.digest(self.bundle / line.split()[1])+"  "+line.split()[1] for line in manifest.read_text().splitlines()]
        manifest.write_text("\n".join(lines)+"\n")
        before = len(self.commands())
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        mutation = [args[:2] for args in self.commands()[before:] if args[0] == "release"]
        self.assertEqual(mutation, [["release", "download"]])
        self.assertNotEqual((self.root / "published/install.sh").read_bytes(), (self.bundle / "install.sh").read_bytes())

    def test_tag_created_before_a_failed_release_can_be_resumed(self):
        self.state(create_fail=True)
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        self.assertIn(self.sha, self.git("ls-remote", "--tags", "origin"))
        self.state(create_fail=False)
        release.publish("0.3.0", self.bundle)
        self.assertFalse(self.state()["release"]["draft"])

    def test_matching_draft_can_be_resumed_but_a_different_tag_cannot(self):
        self.git("tag", "v0.3.0")
        self.git("push", "origin", "v0.3.0")
        self.state(release={"tag_name": "v0.3.0", "draft": True})
        release.publish("0.3.0", self.bundle)
        self.assertIn(["release", "upload"], [args[:2] for args in self.commands()])
        self.git("commit", "--allow-empty", "-m", "newer fixture")
        os.environ["SOURCE_SHA"] = self.git("rev-parse", "HEAD")
        with self.assertRaises(release.ReleaseError): release.publish("0.3.0", self.bundle)
        self.assertIn(self.sha, self.git("ls-remote", "--tags", "origin"))

    def test_prerelease_is_marked_before_publication(self):
        self.make_bundle("0.3.0-rc.1")
        release.publish("0.3.0-rc.1", self.bundle)
        self.assertTrue(self.state()["prerelease"])


if __name__ == "__main__":
    unittest.main()

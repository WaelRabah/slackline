"""Installer tests with local release fixtures; no GitHub or Slack requests."""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import textwrap
import unittest

INSTALLER = Path(__file__).resolve().parents[1] / "install.sh"

CURL = r'''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
args = sys.argv[1:]
url = args[-1]
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps({'url': url}) + '\n')
if '--write-out' in args:
    print('https://github.com/WaelRabah/slackline/releases/tag/v0.1.0', end='')
    sys.exit(0)
out = pathlib.Path(args[args.index('--output') + 1])
root = pathlib.Path(os.environ['FAKE_RELEASE'])
name = url.rsplit('/', 1)[1]
shutil.copyfile(root / name, out)
'''

class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tools = self.root / "tools"
        self.release = self.root / "release"
        self.destination = self.root / "installed bin"
        self.scratch = self.root / "scratch"
        for path in [self.tools, self.release, self.scratch]:
            path.mkdir()
        self.env = os.environ.copy()
        for key in ["GH_TOKEN", "GITHUB_TOKEN", "GH_DEBUG", "SLACKLINE_VERSION", "SLACKLINE_INSTALL_DIR"]:
            self.env.pop(key, None)
        self.env.update({
            "PATH": str(self.tools) + os.pathsep + self.env["PATH"],
            "SLACKLINE_INSTALL_DIR": str(self.destination),
            "TMPDIR": str(self.scratch),
            "FAKE_RELEASE": str(self.release),
            "FAKE_LOG": str(self.root / "requests.jsonl"),
            "FAKE_OS": "Linux",
            "FAKE_CPU": "x86_64",
        })
        self.tool("curl", CURL)
        self.tool("uname", '#!/bin/sh\nif [ "$1" = -s ]; then printf "%s\\n" "$FAKE_OS"; else printf "%s\\n" "$FAKE_CPU"; fi\n')
        self.fixture()

    def tool(self, name, content):
        path = self.tools / name
        path.write_text(textwrap.dedent(content))
        path.chmod(0o755)

    def fixture(self, platform="linux", arch="amd64", binary_version="0.1.0"):
        name = "slackline_0.1.0_{}_{}.tar.gz".format(platform, arch)
        self.env["FAKE_ARCHIVE"] = name
        body = ('#!/bin/sh\nprintf "Slackline %s\\n" "' + binary_version + '"\n').encode()
        with tarfile.open(self.release / name, "w:gz") as archive:
            entry = tarfile.TarInfo("slackline")
            entry.size = len(body)
            entry.mode = 0o755
            archive.addfile(entry, io.BytesIO(body))
        digest = hashlib.sha256((self.release / name).read_bytes()).hexdigest()
        (self.release / "checksums.txt").write_text(digest + "  " + name + "\n")

    def run_installer(self):
        return subprocess.run(["sh", str(INSTALLER)], env=self.env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def assert_installed(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        binary = self.destination / "slackline"
        self.assertTrue(os.access(binary, os.X_OK))
        self.assertEqual(subprocess.check_output([str(binary), "--version"], text=True).strip(), "Slackline 0.1.0")
        self.assertFalse(list(self.scratch.iterdir()))
        self.assertFalse(list(self.destination.glob(".slackline.*")))

    def test_public_latest_and_all_platform_architecture_names(self):
        for system, cpu, platform, arch in [("Linux", "x86_64", "linux", "amd64"), ("Linux", "aarch64", "linux", "arm64"), ("Darwin", "arm64", "darwin", "arm64"), ("Darwin", "x86_64", "darwin", "amd64")]:
            with self.subTest(system=system, cpu=cpu):
                self.env.update(FAKE_OS=system, FAKE_CPU=cpu)
                self.fixture(platform, arch)
                self.assert_installed(self.run_installer())

    def test_public_installation_ignores_github_credentials_and_cli(self):
        self.env["GH_TOKEN"] = "unused-fixture-token"
        self.tool("gh", "#!/bin/sh\nexit 99\n")
        result = self.run_installer()
        self.assert_installed(result)
        self.assertNotIn("unused-fixture-token", result.stdout + result.stderr)

    def test_pinned_version_does_not_resolve_latest(self):
        self.env["SLACKLINE_VERSION"] = "0.1.0"
        self.assert_installed(self.run_installer())
        self.assertNotIn("/latest", (self.root / "requests.jsonl").read_text())

    def test_bad_checksum_keeps_existing_installation(self):
        self.destination.mkdir()
        old = self.destination / "slackline"
        old.write_text("existing binary")
        (self.release / "checksums.txt").write_text("0" * 64 + "  " + self.env["FAKE_ARCHIVE"] + "\n")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Checksum mismatch", result.stderr)
        self.assertEqual(old.read_text(), "existing binary")
        self.assertFalse(list(self.scratch.iterdir()))

    def test_binary_version_mismatch_prevents_installation(self):
        self.fixture(binary_version="9.9.9")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("version does not match", result.stderr)
        self.assertFalse(self.destination.exists())

    def test_unsupported_architecture_never_downloads(self):
        self.env["FAKE_CPU"] = "riscv64"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "requests.jsonl").exists())

    def test_invalid_version_does_not_download(self):
        self.env["SLACKLINE_VERSION"] = "v0.1.0/../../other"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "requests.jsonl").exists())


if __name__ == "__main__":
    unittest.main()

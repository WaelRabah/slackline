#!/usr/bin/env bash
# Build an unsigned macOS installer from a GoReleaser archive.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo 'Usage: package-macos.sh <darwin.tar.gz> <version> <output-directory>' >&2
  exit 1
fi
archive=$1
version=$2
output=$3
case "$(basename "$archive")" in
  *_darwin_amd64.tar.gz|*_darwin_arm64.tar.gz) ;;
  *) echo 'Expected a Slackline macOS archive' >&2; exit 1 ;;
esac
package_version=${version%%-*}
if [[ ! $package_version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Installer version must start with major.minor.patch' >&2
  exit 1
fi
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/archive" "$stage/root/usr/local/bin" "$stage/root/usr/local/share/doc/slackline" "$output"
tar -xzf "$archive" -C "$stage/archive" slackline LICENSE README.md config.example.toml
install -m 0755 "$stage/archive/slackline" "$stage/root/usr/local/bin/slackline"
for name in LICENSE README.md config.example.toml; do
  install -m 0644 "$stage/archive/$name" "$stage/root/usr/local/share/doc/slackline/$name"
done
package_name=$(basename "$archive" .tar.gz).pkg
pkgbuild --root "$stage/root" \
  --identifier io.github.waelrabah.slackline \
  --version "$package_version" \
  --ownership recommended \
  --install-location / \
  "$output/$package_name"
pkgutil --check-signature "$output/$package_name" || status=$?
# pkgutil exits 1 for the deliberately unsigned installer; other errors fail.
if [[ ${status:-0} -gt 1 ]]; then exit "$status"; fi

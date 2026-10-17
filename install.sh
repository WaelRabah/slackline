#!/bin/sh
# Install a verified Slackline release for the current user. No sudo required.
# Public repository: curl -fsSL https://github.com/WaelRabah/slackline/releases/latest/download/install.sh | sh
set -eu

fail() { printf 'Slackline installer: %s\n' "$*" >&2; exit 1; }

main() {
  repo=WaelRabah/slackline
  web=https://github.com/$repo
  stage=''
  staged_binary=''
  cleanup() {
    if [ -n "$staged_binary" ]; then rm -f "$staged_binary"; fi
    if [ -n "$stage" ]; then rm -rf "$stage"; fi
  }
  trap cleanup 0
  trap 'exit 130' INT
  trap 'exit 143' HUP TERM
  umask 077

  case $(uname -s) in Darwin) platform=darwin ;; Linux) platform=linux ;; *) fail 'Only macOS and Linux are supported' ;; esac
  case $(uname -m) in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) fail 'This CPU architecture is not supported (amd64/arm64 only)' ;; esac
  for tool in curl tar awk mktemp; do command -v "$tool" >/dev/null 2>&1 || fail "Required command is missing: $tool"; done
  if command -v sha256sum >/dev/null 2>&1; then checksum=sha256sum
  elif command -v shasum >/dev/null 2>&1; then checksum=shasum
  else fail 'A SHA-256 tool is required: sha256sum or shasum'; fi

  tag=${SLACKLINE_VERSION:-}
  if [ -n "$tag" ]; then
    case "$tag" in v*) ;; *) tag=v$tag ;; esac
    valid_tag "$tag" || fail 'SLACKLINE_VERSION must be a release version such as v0.1.0'
  fi
  destination=${SLACKLINE_INSTALL_DIR:-${HOME:?HOME must be set}/.local/bin}
  case "$destination" in /*) ;; *) destination=$(pwd)/$destination ;; esac
  stage=$(mktemp -d "${TMPDIR:-/tmp}/slackline-install.XXXXXX")

  if [ -z "$tag" ]; then
    latest=$(curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
      --connect-timeout 10 --max-time 60 --retry 2 --output /dev/null --write-out '%{url_effective}' "$web/releases/latest") || fail 'Cannot find a published release; check your connection and https://github.com/WaelRabah/slackline/releases'
    case "$latest" in "$web/releases/tag/"*) tag=${latest##*/} ;; *) fail 'GitHub did not return a versioned release' ;; esac
  fi
  valid_tag "$tag" || fail 'GitHub returned an unsupported release version'
  release_version=${tag#v}
  archive=slackline_${release_version}_${platform}_${arch}.tar.gz
  printf 'Installing Slackline %s for %s/%s…\n' "$release_version" "$platform" "$arch"

  for asset in "$archive" checksums.txt; do
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
      --connect-timeout 10 --max-time 180 --retry 2 --output "$stage/$asset" \
      "$web/releases/download/$tag/$asset" || fail "Could not download $asset; check the release exists and your connection"
  done

  expected=$(awk -v name="$archive" '$2 == name {print $1; n++} END {if (n != 1) exit 1}' "$stage/checksums.txt") || fail 'Archive is missing from checksums.txt, or has duplicate entries'
  printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' || fail 'Release checksum is invalid'
  if [ "$checksum" = sha256sum ]; then actual=$(sha256sum "$stage/$archive" | awk '{print $1}')
  else actual=$(shasum -a 256 "$stage/$archive" | awk '{print $1}'); fi
  [ "$actual" = "$expected" ] || fail 'Checksum mismatch; existing installation was not changed'

  tar -xzf "$stage/$archive" -C "$stage" slackline || fail 'Could not extract the release binary'
  [ -f "$stage/slackline" ] && [ ! -L "$stage/slackline" ] || fail 'Archive does not contain a regular Slackline executable'
  chmod 0755 "$stage/slackline"
  installed_version=$("$stage/slackline" --version) || fail 'The downloaded binary cannot run on this machine'
  [ "$installed_version" = "Slackline $release_version" ] || fail 'Downloaded binary version does not match the requested release'

  mkdir -p "$destination" || fail 'Cannot create the install directory; choose a writable SLACKLINE_INSTALL_DIR'
  [ ! -d "$destination/slackline" ] || fail 'Install target is a directory'
  staged_binary=$(mktemp "$destination/.slackline.XXXXXX") || fail 'Install directory is not writable; choose SLACKLINE_INSTALL_DIR (no automatic sudo)'
  cp "$stage/slackline" "$staged_binary"
  chmod 0755 "$staged_binary"
  mv -f "$staged_binary" "$destination/slackline"
  staged_binary=''
  printf 'Installed %s to %s/slackline\n' "$installed_version" "$destination"
  case :${PATH:-}: in *:"$destination":*) printf 'Run: slackline auth login\n' ;; *) printf 'Add %s to PATH, then run: slackline auth login\n' "$destination" ;; esac
}

valid_tag() { printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; }

# Kept last so a truncated download cannot begin installation midway through.
main "$@"

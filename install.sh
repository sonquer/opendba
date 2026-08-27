#!/bin/sh

set -eu

REPO="sonquer/opendba"
BINARY="opendba"
VERSION="${OPENDBA_VERSION:-latest}"
INSTALL_DIR="${OPENDBA_INSTALL_DIR:-$HOME/.local/bin}"
FORCE="${OPENDBA_FORCE:-}"

say() { printf '%s\n' "$*"; }
die() { printf '%s\n' "$*" >&2; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "this needs $1, which is not on the PATH"
}

platform() {
	os="$(uname -s)"
	arch="$(uname -m)"
	case "$os" in
		Linux) os=linux ;;
		Darwin) os=darwin ;;
		*) die "no build for $os. Windows has install.ps1, and everything else has go install." ;;
	esac
	case "$arch" in
		x86_64 | amd64) arch=amd64 ;;
		arm64 | aarch64) arch=arm64 ;;
		*) die "no build for $arch. There are builds for amd64 and arm64." ;;
	esac
	printf '%s_%s' "$os" "$arch"
}

release() {
	if [ "$VERSION" = "latest" ]; then
		url="https://api.github.com/repos/$REPO/releases/latest"
	else
		url="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
	fi
	if [ -n "${GITHUB_TOKEN:-}" ]; then
		curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "$url"
	else
		curl -fsSL "$url"
	fi
}

asset() {
	sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p' "$1" \
		| grep -- "$2" \
		| head -n 1
}

installed() {
	for candidate in "$INSTALL_DIR/$BINARY" "$(command -v "$BINARY" 2>/dev/null || true)"; do
		if [ -n "$candidate" ] && [ -x "$candidate" ]; then
			"$candidate" version 2>/dev/null | head -n 1 | cut -d' ' -f1
			return 0
		fi
	done
	return 0
}

offered() {
	basename "$1" | sed -e "s/^${BINARY}_//" -e "s/_${2}\.tar\.gz\$//"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "this needs sha256sum or shasum to check what it downloaded"
	fi
}

verify() {
	archive="$1"
	sums="$2"
	name="$(basename "$archive")"
	want="$(grep -- " $name\$" "$sums" | cut -d' ' -f1 || true)"
	[ -n "$want" ] || die "$name is not in checksums.txt"
	got="$(sha256 "$archive")"
	[ "$want" = "$got" ] || die "$name does not match its checksum
  expected $want
  got      $got"
	say "  checksum ok"
}

signature() {
	sums="$1"
	bundle="$2"
	if [ ! -f "$bundle" ] || ! command -v cosign >/dev/null 2>&1; then
		return 0
	fi
	if cosign verify-blob "$sums" \
		--bundle "$bundle" \
		--certificate-identity-regexp "^https://github.com/$REPO/" \
		--certificate-oidc-issuer https://token.actions.githubusercontent.com \
		>/dev/null 2>&1; then
		say "  signature ok"
	else
		die "the signature on checksums.txt does not verify"
	fi
}

main() {
	need curl
	need tar
	target="$(platform)"

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT INT TERM

	say "opendba"
	say "  looking up $VERSION"
	release > "$work/release.json" || die "no release called $VERSION"

	tag="$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$work/release.json" | head -n 1)"
	archive_url="$(asset "$work/release.json" "_${target}.tar.gz")"
	[ -n "$archive_url" ] || die "$tag has no build for $target"
	sums_url="$(asset "$work/release.json" "checksums.txt")"
	bundle_url="$(asset "$work/release.json" "checksums.txt.sigstore.json")"

	want="$(offered "$archive_url" "$target")"
	have="$(installed)"
	if [ -n "$have" ] && [ "$have" = "$want" ] && [ -z "$FORCE" ]; then
		say "  $want is already installed"
		return 0
	fi
	if [ -n "$have" ]; then
		say "  $have is installed, $want is what $tag holds"
	fi

	archive="$work/$(basename "$archive_url")"
	say "  downloading $(basename "$archive_url") from $tag"
	curl -fsSL -o "$archive" "$archive_url"

	if [ -n "$sums_url" ]; then
		curl -fsSL -o "$work/checksums.txt" "$sums_url"
		if [ -n "$bundle_url" ]; then
			curl -fsSL -o "$work/checksums.txt.sigstore.json" "$bundle_url"
		fi
		signature "$work/checksums.txt" "$work/checksums.txt.sigstore.json"
		verify "$archive" "$work/checksums.txt"
	else
		say "  this release publishes no checksums, so nothing was checked"
	fi

	tar -xzf "$archive" -C "$work"
	[ -f "$work/$BINARY" ] || die "the archive holds no $BINARY"

	mkdir -p "$INSTALL_DIR"
	install -m 0755 "$work/$BINARY" "$INSTALL_DIR/$BINARY" 2>/dev/null \
		|| { cp "$work/$BINARY" "$INSTALL_DIR/$BINARY" && chmod 0755 "$INSTALL_DIR/$BINARY"; }

	say "  installed to $INSTALL_DIR/$BINARY"

	case ":$PATH:" in
		*":$INSTALL_DIR:"*) say "" && say "Run: $BINARY" ;;
		*)
			say ""
			say "$INSTALL_DIR is not on your PATH. Add it:"
			say ""
			say "  export PATH=\"\$PATH:$INSTALL_DIR\""
			;;
	esac
}

while [ $# -gt 0 ]; do
	case "$1" in
		--nightly) VERSION=nightly ;;
		--version) shift; VERSION="${1:-}" ;;
		--dir) shift; INSTALL_DIR="${1:-}" ;;
		--force) FORCE=1 ;;
		-h | --help)
			say "usage: install.sh [--nightly] [--version <tag>] [--dir <path>] [--force]"
			exit 0
			;;
		*) die "unknown option: $1" ;;
	esac
	shift
done

[ -n "$VERSION" ] || die "--version needs a tag"
[ -n "$INSTALL_DIR" ] || die "--dir needs a path"

main

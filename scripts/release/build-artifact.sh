#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-}"
GOOS="${GOOS:-}"
GOARCH="${GOARCH:-}"
OUTPUT_DIR="${OUTPUT_DIR:-dist}"

if [[ -z "${VERSION}" ]]; then
  echo "VERSION is required" >&2
  exit 1
fi

if [[ -z "${GOOS}" || -z "${GOARCH}" ]]; then
  echo "GOOS and GOARCH are required" >&2
  exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [[ -z "${SOURCE_DATE_EPOCH:-}" ]]; then
  SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
fi

VERSION_NO_V="${VERSION#v}"
PACKAGE_NAME="jira-issue-sync_${VERSION_NO_V}_${GOOS}_${GOARCH}.tar.gz"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

BINARY_PATH="${WORKDIR}/jira-issue-sync"

CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
  go build -trimpath -ldflags='-buildid=' -o "${BINARY_PATH}" ./cmd/jira-issue-sync

mkdir -p "${OUTPUT_DIR}"

python3 - "${BINARY_PATH}" "${OUTPUT_DIR}/${PACKAGE_NAME}" "${SOURCE_DATE_EPOCH}" <<'PY'
import gzip
import tarfile
import sys

binary_path = sys.argv[1]
archive_path = sys.argv[2]
epoch = int(sys.argv[3])

with open(archive_path, "wb") as raw_file:
    with gzip.GzipFile(filename="", mode="wb", fileobj=raw_file, mtime=epoch) as gz_file:
        with tarfile.open(fileobj=gz_file, mode="w", format=tarfile.PAX_FORMAT) as tar:
            info = tar.gettarinfo(binary_path, arcname="jira-issue-sync")
            info.uid = 0
            info.gid = 0
            info.uname = ""
            info.gname = ""
            info.mtime = epoch
            info.mode = 0o755
            with open(binary_path, "rb") as binary_file:
                tar.addfile(info, binary_file)
PY

if command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM="$(sha256sum "${OUTPUT_DIR}/${PACKAGE_NAME}" | awk '{print $1}')"
else
  CHECKSUM="$(shasum -a 256 "${OUTPUT_DIR}/${PACKAGE_NAME}" | awk '{print $1}')"
fi

printf '%s\n' "${CHECKSUM}" > "${OUTPUT_DIR}/${PACKAGE_NAME}.sha256"

echo "built ${OUTPUT_DIR}/${PACKAGE_NAME}"

#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-}"
GOOS="${GOOS:-}"
GOARCH="${GOARCH:-}"

if [[ -z "${VERSION}" || -z "${GOOS}" || -z "${GOARCH}" ]]; then
  echo "VERSION, GOOS, and GOARCH are required" >&2
  exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

if [[ -z "${SOURCE_DATE_EPOCH:-}" ]]; then
  SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
fi

ARTIFACT_NAME="jira-issue-sync_${VERSION#v}_${GOOS}_${GOARCH}.tar.gz"
FIRST_DIR="$(mktemp -d)"
SECOND_DIR="$(mktemp -d)"
trap 'rm -rf "${FIRST_DIR}" "${SECOND_DIR}"' EXIT

VERSION="${VERSION}" GOOS="${GOOS}" GOARCH="${GOARCH}" OUTPUT_DIR="${FIRST_DIR}" SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH}" \
  ./scripts/release/build-artifact.sh >/dev/null
VERSION="${VERSION}" GOOS="${GOOS}" GOARCH="${GOARCH}" OUTPUT_DIR="${SECOND_DIR}" SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH}" \
  ./scripts/release/build-artifact.sh >/dev/null

if command -v sha256sum >/dev/null 2>&1; then
  FIRST_SUM="$(sha256sum "${FIRST_DIR}/${ARTIFACT_NAME}" | awk '{print $1}')"
  SECOND_SUM="$(sha256sum "${SECOND_DIR}/${ARTIFACT_NAME}" | awk '{print $1}')"
else
  FIRST_SUM="$(shasum -a 256 "${FIRST_DIR}/${ARTIFACT_NAME}" | awk '{print $1}')"
  SECOND_SUM="$(shasum -a 256 "${SECOND_DIR}/${ARTIFACT_NAME}" | awk '{print $1}')"
fi

if [[ "${FIRST_SUM}" != "${SECOND_SUM}" ]]; then
  echo "reproducibility check failed for ${ARTIFACT_NAME}" >&2
  echo "first:  ${FIRST_SUM}" >&2
  echo "second: ${SECOND_SUM}" >&2
  exit 1
fi

echo "reproducibility check passed for ${ARTIFACT_NAME} (${FIRST_SUM})"

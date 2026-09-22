#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dockerfile_path="${project_root}/packaging/windows/Dockerfile.build"
temporary_output_directory="$(mktemp -d)"
builder_image_name="compasso-windows-binaries:local"
temporary_container_id=""

cleanup_temporary_output() {
  if [[ -n "${temporary_container_id}" ]]; then
    docker rm --force "${temporary_container_id}" >/dev/null 2>&1 || true
  fi
  rm -rf "${temporary_output_directory}"
}
trap cleanup_temporary_output EXIT

docker build \
  --file "${dockerfile_path}" \
  --target exported-binaries \
  --tag "${builder_image_name}" \
  "${project_root}"

temporary_container_id="$(docker create "${builder_image_name}" /compasso-windows-service.exe)"
for binary_name in compasso-windows-service.exe compasso-windows-companion.exe; do
  docker cp \
    "${temporary_container_id}:/${binary_name}" \
    "${temporary_output_directory}/${binary_name}"
  test -s "${temporary_output_directory}/${binary_name}"
done

install -d "${project_root}/dist/windows"
install -m 0755 \
  "${temporary_output_directory}/compasso-windows-service.exe" \
  "${temporary_output_directory}/compasso-windows-companion.exe" \
  "${project_root}/dist/windows/"

echo "Binários Windows criados em ${project_root}/dist/windows."

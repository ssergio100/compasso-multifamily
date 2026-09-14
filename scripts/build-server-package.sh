#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
default_version="$(sed -n 's/^Version: //p' "${project_root}/packaging/debian/control")"
package_version="${1:-${default_version}}"
package_architecture="all"
container_version="${package_version//\~/-}"

if ! dpkg --validate-version "${package_version}"; then
  echo "erro: versão Debian inválida: ${package_version}" >&2
  exit 1
fi
if [[ ! "${container_version}" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]; then
  echo "erro: versão não pode ser convertida em uma tag Docker válida: ${package_version}" >&2
  exit 1
fi

temporary_directory="$(mktemp -d)"
trap 'rm -rf "${temporary_directory}"' EXIT
package_root="${temporary_directory}/compasso-server"
application_root="${package_root}/opt/compasso-server"

install -d \
  "${package_root}/DEBIAN" \
  "${package_root}/etc/compasso-server" \
  "${package_root}/usr/share/doc/compasso-server/third-party" \
  "${package_root}/usr/lib/systemd/system" \
  "${application_root}/agent" \
  "${application_root}/licenses/third-party" \
  "${application_root}/scripts" \
  "${application_root}/docs" \
  "${project_root}/dist"

sed "s/@VERSION@/${package_version}/g" \
  "${project_root}/packaging/server-debian/control" \
  > "${package_root}/DEBIAN/control"
install -m 0644 "${project_root}/packaging/server-debian/conffiles" \
  "${package_root}/DEBIAN/conffiles"
install -m 0755 "${project_root}/packaging/server-debian/postinst" \
  "${package_root}/DEBIAN/postinst"

install -m 0644 "${project_root}/go.mod" "${project_root}/go.sum" \
  "${project_root}/compose.yaml" "${project_root}/.dockerignore" \
  "${project_root}/LICENSE" "${project_root}/THIRD_PARTY_NOTICES.md" \
  "${application_root}/"
install -m 0644 "${project_root}"/licenses/third-party/* \
  "${application_root}/licenses/third-party/"
install -m 0644 "${project_root}/LICENSE" \
  "${package_root}/usr/share/doc/compasso-server/LICENSE"
install -m 0644 "${project_root}/THIRD_PARTY_NOTICES.md" \
  "${package_root}/usr/share/doc/compasso-server/THIRD_PARTY_NOTICES.md"
install -m 0644 "${project_root}/packaging/copyright" \
  "${package_root}/usr/share/doc/compasso-server/copyright"
install -m 0644 "${project_root}"/licenses/third-party/* \
  "${package_root}/usr/share/doc/compasso-server/third-party/"
install -m 0644 "${project_root}/.env.server.example" \
  "${package_root}/etc/compasso-server/compasso.env"
sed -i "s/^COMPASSO_VERSION=.*/COMPASSO_VERSION=${container_version}/" \
  "${package_root}/etc/compasso-server/compasso.env"
ln -s /etc/compasso-server/compasso.env "${application_root}/.env"

cp -a "${project_root}/agent/localauth" "${project_root}/agent/policy" \
  "${project_root}/agent/storage" "${application_root}/agent/"
cp -a "${project_root}/protocol" "${project_root}/server" "${application_root}/"
rm -f "${application_root}/server/config.toml"
find "${application_root}" -type f \
  \( -name '*_test.go' -o -name '*.test.js' \) -delete

server_scripts=(
  install-server.sh backup-server.sh restore-server-backup.sh update-server.sh
  status-server.sh
)
for script_name in "${server_scripts[@]}"; do
  install -m 0755 "${project_root}/scripts/${script_name}" \
    "${application_root}/scripts/${script_name}"
done
install -m 0644 "${project_root}/docs/server-installation.md" \
  "${application_root}/README.md"
install -m 0644 "${project_root}/docs/atualizacao-manual-servidor.md" \
  "${application_root}/docs/"
install -m 0644 \
  "${project_root}/packaging/systemd/compasso-server-backup.service" \
  "${project_root}/packaging/systemd/compasso-server-backup.timer" \
  "${package_root}/usr/lib/systemd/system/"

package_path="${project_root}/dist/compasso-server_${package_version}_${package_architecture}.deb"
dpkg-deb --root-owner-group --build "${package_root}" "${package_path}"
package_name="$(basename "${package_path}")"
(
  cd "${project_root}/dist"
  sha256sum "${package_name}" > "${package_name}.sha256"
)

echo "Pacote Debian do servidor criado."
echo "  Versão: ${package_version}"
echo "  Arquivo: ${package_path}"
echo "  SHA-256: $(cut -d' ' -f1 "${package_path}.sha256")"

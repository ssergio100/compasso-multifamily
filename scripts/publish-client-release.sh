#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deployment_config="${COMPASSO_CLIENT_RELEASE_DEPLOY_CONFIG:-${project_root}/.private/deploy/client-release.env}"
remote_target="${COMPASSO_CLIENT_RELEASE_DEPLOY_TARGET:-}"
remote_directory="${COMPASSO_CLIENT_RELEASE_DEPLOY_DIRECTORY:-/srv/sites/compasso-admin-ui/downloads}"
public_base_url="${COMPASSO_CLIENT_RELEASE_PUBLIC_BASE_URL:-}"
assume_yes=false

if [[ -f "${deployment_config}" ]]; then
  while IFS='=' read -r key value; do
    case "${key}" in
      COMPASSO_CLIENT_RELEASE_DEPLOY_TARGET)
        [[ -n "${remote_target}" ]] || remote_target="${value}"
        ;;
      COMPASSO_CLIENT_RELEASE_DEPLOY_DIRECTORY)
        remote_directory="${value:-${remote_directory}}"
        ;;
      COMPASSO_CLIENT_RELEASE_PUBLIC_BASE_URL)
        [[ -n "${public_base_url}" ]] || public_base_url="${value}"
        ;;
    esac
  done < "${deployment_config}"
fi

show_usage() {
  cat <<'EOF'
Uso: ./scripts/publish-client-release.sh [opções]

Gera, valida e publica o pacote Debian do agente. O manifesto é publicado por
último, de forma atômica, para que a interface nunca aponte para um arquivo
incompleto.

Opções:
  --target USUARIO@HOST  destino aceito por ssh/scp
  --directory CAMINHO   diretório remoto de downloads
  --public-base-url URL  origem HTTPS da interface, sem caminho
  --yes                  não pede confirmação antes do envio
  -h, --help             mostra esta ajuda

Variáveis equivalentes: COMPASSO_CLIENT_RELEASE_DEPLOY_TARGET,
COMPASSO_CLIENT_RELEASE_DEPLOY_DIRECTORY e
COMPASSO_CLIENT_RELEASE_PUBLIC_BASE_URL.
Por padrão, o script também lê .private/deploy/client-release.env, se existir.
EOF
}

fail() {
  echo "erro: $*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target) [[ $# -ge 2 ]] || fail "--target exige um valor"; remote_target="$2"; shift 2 ;;
    --directory) [[ $# -ge 2 ]] || fail "--directory exige um valor"; remote_directory="$2"; shift 2 ;;
    --public-base-url) [[ $# -ge 2 ]] || fail "--public-base-url exige um valor"; public_base_url="$2"; shift 2 ;;
    --yes) assume_yes=true; shift ;;
    -h|--help) show_usage; exit 0 ;;
    *) fail "opção desconhecida: $1" ;;
  esac
done

[[ -n "${remote_target}" ]] || fail "informe --target ou COMPASSO_CLIENT_RELEASE_DEPLOY_TARGET"
[[ "${remote_target}" =~ ^([A-Za-z0-9._-]+@)?[A-Za-z0-9._-]+$ ]] \
  || fail "destino SSH inválido: ${remote_target}"
[[ "${remote_directory}" =~ ^/[A-Za-z0-9._/-]+$ && "${remote_directory}" != *".."* ]] \
  || fail "diretório remoto deve ser um caminho absoluto simples"
[[ "${public_base_url}" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] \
  || fail "a URL pública deve ser uma origem HTTPS sem caminho, consulta ou fragmento"

for command_name in cmp curl dpkg-deb scp sha256sum ssh; do
  command -v "${command_name}" >/dev/null 2>&1 \
    || fail "comando obrigatório não encontrado: ${command_name}"
done

echo "Gerando os binários portáteis do agente..."
"${project_root}/scripts/build-portable-client-binaries.sh"
"${project_root}/scripts/build-debian-package.sh"

package_version="$(sed -n 's/^Version: //p' "${project_root}/packaging/debian/control")"
package_architecture="$(sed -n 's/^Architecture: //p' "${project_root}/packaging/debian/control")"
package_path="${project_root}/dist/compasso-client_${package_version}_${package_architecture}.deb"
checksum_path="${package_path}.sha256"
"${project_root}/scripts/test-debian-package.sh" "${package_path}"

[[ -f "${checksum_path}" ]] || fail "checksum não gerado: ${checksum_path}"
artifact_name="$(basename "${package_path}")"
checksum_name="$(basename "${checksum_path}")"
[[ "${artifact_name}" =~ ^[A-Za-z0-9.+_~-]+\.deb$ ]] || fail "nome de pacote inseguro"
checksum="$(cut -d' ' -f1 "${checksum_path}")"
[[ "${checksum}" =~ ^[a-f0-9]{64}$ ]] || fail "checksum SHA-256 inválido"

temporary_directory="$(mktemp -d)"
cleanup() {
  rm -rf "${temporary_directory}"
}
trap cleanup EXIT

manifest_path="${temporary_directory}/agent-release.json"
published_at="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
cat > "${manifest_path}" <<EOF
{
  "version": "${package_version}",
  "architecture": "${package_architecture}",
  "package_format": "deb",
  "download_url": "/downloads/${artifact_name}",
  "checksum_url": "/downloads/${checksum_name}",
  "sha256": "${checksum}",
  "published_at": "${published_at}"
}
EOF

echo
echo "Publicação do agente"
echo "  Pacote: ${artifact_name}"
echo "  SHA-256: ${checksum}"
echo "  Destino: ${remote_target}:${remote_directory}"
echo "  Link: ${public_base_url}/downloads/${artifact_name}"
if [[ "${assume_yes}" == false ]]; then
  read -r -p "Publicar esta versão? [S/n] " answer
  [[ -z "${answer}" || "${answer}" =~ ^[sS]$ ]] || fail "publicação cancelada"
fi

ssh -o BatchMode=yes "${remote_target}" \
  "install -d -m 0755 '${remote_directory}' && test -w '${remote_directory}'" \
  || fail "diretório remoto ausente ou sem permissão de escrita"

remote_existing_checksum="$(ssh -o BatchMode=yes "${remote_target}" \
  "if test -f '${remote_directory}/${artifact_name}'; then sha256sum '${remote_directory}/${artifact_name}' | cut -d' ' -f1; fi")"
if [[ -n "${remote_existing_checksum}" && "${remote_existing_checksum}" != "${checksum}" ]]; then
  fail "a versão ${package_version} já existe no servidor com outro conteúdo; incremente a versão"
fi

upload_suffix=".upload-$$"
if [[ -z "${remote_existing_checksum}" ]]; then
  echo "Enviando pacote e checksum..."
  scp -o BatchMode=yes "${package_path}" \
    "${remote_target}:${remote_directory}/${artifact_name}${upload_suffix}"
  scp -o BatchMode=yes "${checksum_path}" \
    "${remote_target}:${remote_directory}/${checksum_name}${upload_suffix}"
  ssh -o BatchMode=yes "${remote_target}" "set -eu
actual_checksum=\$(sha256sum '${remote_directory}/${artifact_name}${upload_suffix}' | cut -d' ' -f1)
test \"\${actual_checksum}\" = '${checksum}'
chmod 0644 '${remote_directory}/${artifact_name}${upload_suffix}' '${remote_directory}/${checksum_name}${upload_suffix}'
mv '${remote_directory}/${artifact_name}${upload_suffix}' '${remote_directory}/${artifact_name}'
mv '${remote_directory}/${checksum_name}${upload_suffix}' '${remote_directory}/${checksum_name}'"
else
  echo "O pacote idêntico já existe no servidor; mantendo o arquivo publicado."
fi

echo "Publicando o manifesto..."
scp -o BatchMode=yes "${manifest_path}" \
  "${remote_target}:${remote_directory}/agent-release.json${upload_suffix}"
ssh -o BatchMode=yes "${remote_target}" \
  "chmod 0644 '${remote_directory}/agent-release.json${upload_suffix}' && mv '${remote_directory}/agent-release.json${upload_suffix}' '${remote_directory}/agent-release.json'"

echo "Validando os endereços públicos..."
curl --fail --silent --show-error --max-time 20 \
  --output "${temporary_directory}/published-manifest.json" \
  "${public_base_url}/downloads/agent-release.json"
cmp --silent "${manifest_path}" "${temporary_directory}/published-manifest.json" \
  || fail "o manifesto público não corresponde ao arquivo enviado"
curl --fail --silent --show-error --max-time 60 \
  --output /dev/null "${public_base_url}/downloads/${artifact_name}"

echo
echo "Agente publicado com sucesso."
echo "  Versão: ${package_version}"
echo "  Download: ${public_base_url}/downloads/${artifact_name}"
echo "  Manifesto: ${public_base_url}/downloads/agent-release.json"

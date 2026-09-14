#!/usr/bin/env bash
set -euo pipefail

project_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "${EUID}" -ne 0 ]]; then
  echo "erro: execute com sudo: sudo ./scripts/install-server.sh" >&2
  exit 1
fi

install_docker_if_authorized() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    return
  fi
  echo "Docker Engine e Docker Compose são necessários e não foram encontrados."
  read -r -p "Autoriza instalar pelos repositórios do Debian? [s/N] " authorization
  if [[ ! "${authorization}" =~ ^[sS]$ ]]; then
    echo "instalação cancelada sem alterar o sistema"
    exit 1
  fi
  if [[ ! -r /etc/os-release ]] || ! grep -q '^ID=debian$' /etc/os-release; then
    echo "erro: instalação automática de dependências é suportada somente no Debian" >&2
    exit 1
  fi
  apt-get update
  compose_package=""
  for candidate in docker-compose-plugin docker-compose-v2 docker-compose; do
    if apt-cache show "${candidate}" >/dev/null 2>&1; then compose_package="${candidate}"; break; fi
  done
  if [[ -z "${compose_package}" ]]; then
    echo "erro: plugin Docker Compose não encontrado nos repositórios configurados" >&2
    exit 1
  fi
  apt-get install --yes docker.io "${compose_package}"
  systemctl enable --now docker.service
  docker compose version
}

install_docker_if_authorized
if [[ ! -f "${project_directory}/.env" ]]; then
  install -o root -g root -m 0644 "${project_directory}/.env.server.example" "${project_directory}/.env"
fi

read_environment_setting() {
  local setting_name="$1"
  local fallback_value="$2"
  local configured_value
  configured_value="$(awk -F= -v name="${setting_name}" '$1 == name { sub(/^[^=]*=/, ""); print; exit }' "${project_directory}/.env")"
  printf '%s' "${configured_value:-${fallback_value}}"
}

data_directory="$(read_environment_setting COMPASSO_DATA_DIRECTORY /srv/docker/volumes/compasso)"
backup_directory="$(read_environment_setting COMPASSO_BACKUP_DIRECTORY /srv/docker/backups/compasso)"
mkdir -p "${data_directory}/server" "${backup_directory}"
chown -R 10001:10001 "${data_directory}/server"
chmod 0700 "${data_directory}/server" "${backup_directory}"

docker compose --project-directory "${project_directory}" config --quiet
docker compose --project-directory "${project_directory}" build
docker compose --project-directory "${project_directory}" up --detach --remove-orphans

for attempt in {1..30}; do
  if docker compose --project-directory "${project_directory}" exec --no-TTY compasso-api \
      curl --fail --silent "http://127.0.0.1:8080/healthz" >/dev/null; then
    break
  fi
  if [[ "${attempt}" -eq 30 ]]; then
    docker compose --project-directory "${project_directory}" ps
    echo "erro: serviços não ficaram saudáveis no prazo esperado" >&2
    exit 1
  fi
  sleep 2
done

if [[ -f /usr/lib/systemd/system/compasso-server-backup.timer ]]; then
  systemctl daemon-reload
  systemctl enable --now compasso-server-backup.timer
fi

echo "Compasso instalado."
echo "API: porta 8181 do servidor"
echo "A interface administrativa é implantada separadamente."
echo "Backup diário: compasso-server-backup.timer"

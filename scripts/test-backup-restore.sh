#!/usr/bin/env bash
set -euo pipefail

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "erro: sqlite3 não encontrado no PATH" >&2
  exit 1
fi

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_directory="$(mktemp -d)"
trap 'rm -rf "${test_directory}"' EXIT
data_directory="${test_directory}/data"
backup_directory="${test_directory}/backups"
database_path="${data_directory}/server/tempo-server.db"
backup_path="${backup_directory}/compasso-server-test.tar.gz"

mkdir -p "${data_directory}/server" "${backup_directory}"
for migration in "${project_root}"/server/storage/migrations/*.sql; do
  sqlite3 -bail "${database_path}" < "${migration}"
done
sqlite3 -bail "${database_path}" <<'SQL'
PRAGMA foreign_keys = ON;
INSERT INTO family(id,name,state,created_at,updated_at)
VALUES ('family-backup','Família preservada','active','2026-09-14T12:00:00Z','2026-09-14T12:00:00Z');
INSERT INTO admin_user(
  id,login,password_hash,active,created_at,updated_at,email,email_verified_at,auth_generation
) VALUES (
  'owner-backup','owner@example.com','hash',1,'2026-09-14T12:00:00Z',
  '2026-09-14T12:00:00Z','owner@example.com','2026-09-14T12:00:00Z',1
);
INSERT INTO family_member(family_id,admin_user_id,role,created_at)
VALUES ('family-backup','owner-backup','owner','2026-09-14T12:00:00Z');
SQL

original_checksum="$(sha256sum "${database_path}" | cut -d' ' -f1)"
tar --create --gzip --file "${backup_path}" --directory "${data_directory}" server
tar --list --gzip --file "${backup_path}" >/dev/null

sqlite3 -bail "${database_path}" \
  "UPDATE family SET name='Estado que não pode sobreviver à restauração' WHERE id='family-backup';"
mv "${data_directory}/server" "${backup_directory}/pre-restore-server-test"
tar --extract --gzip --file "${backup_path}" --directory "${data_directory}"

restored_checksum="$(sha256sum "${database_path}" | cut -d' ' -f1)"
integrity="$(sqlite3 "${database_path}" 'PRAGMA integrity_check;')"
foreign_keys="$(sqlite3 "${database_path}" 'PRAGMA foreign_key_check;')"
migration_count="$(sqlite3 "${database_path}" 'SELECT COUNT(*) FROM schema_migrations;')"
family_name="$(sqlite3 "${database_path}" "SELECT name FROM family WHERE id='family-backup';")"

if [[ "${restored_checksum}" != "${original_checksum}" ]]; then
  echo "erro: checksum restaurado difere do backup" >&2
  exit 1
fi
if [[ "${integrity}" != "ok" || -n "${foreign_keys}" ]]; then
  echo "erro: banco restaurado falhou nas verificações de integridade" >&2
  exit 1
fi
if [[ "${migration_count}" -ne 16 || "${family_name}" != "Família preservada" ]]; then
  echo "erro: conteúdo restaurado não corresponde ao banco migrado" >&2
  exit 1
fi

echo "backup/restauração: checksum, conteúdo, 16 migrações, integridade e chaves estrangeiras aprovados"

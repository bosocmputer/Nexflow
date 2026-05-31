#!/usr/bin/env bash
set -euo pipefail

# Apply the SML product image lookup index to a tenant image database.
#
# Usage options:
#   SML_IMAGE_DATABASE_URL='postgresql://postgres:***@192.168.2.248:5432/sml1_2026_images?sslmode=disable' \
#     scripts/apply-sml-image-index.sh
#
#   SML_PGHOST=192.168.2.248 SML_PGUSER=postgres SML_PGPASSWORD='***' SML_TENANT=SML1_2026 \
#     scripts/apply-sml-image-index.sh
#
# Notes:
# - SML_TENANT derives the image DB as lower(SML_TENANT) + '_images'.
# - The SQL uses CREATE INDEX CONCURRENTLY, so this script must not wrap it in
#   a transaction.
# - This script never touches Nexflow's own PostgreSQL database.
# - Set PSQL_BIN when psql is only available through Docker, for example:
#   PSQL_BIN='docker exec -i -e PGPASSWORD nexflow-postgres psql' scripts/apply-sml-image-index.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SQL_FILE="${ROOT_DIR}/scripts/sml-image-index.sql"
PSQL_BIN="${PSQL_BIN:-psql}"
# shellcheck disable=SC2206
PSQL_CMD=(${PSQL_BIN})

if [[ ! -f "${SQL_FILE}" ]]; then
  echo "missing SQL file: ${SQL_FILE}" >&2
  exit 1
fi

USE_DATABASE_URL=false
if [[ -n "${SML_IMAGE_DATABASE_URL:-}" ]]; then
  USE_DATABASE_URL=true
  DATABASE_URL="${SML_IMAGE_DATABASE_URL}"
else
  SML_PGHOST="${SML_PGHOST:-192.168.2.248}"
  SML_PGPORT="${SML_PGPORT:-5432}"
  SML_PGUSER="${SML_PGUSER:-postgres}"
  SML_PGPASSWORD="${SML_PGPASSWORD:-}"
  SML_TENANT="${SML_TENANT:-SML1_2026}"

  if [[ -z "${SML_PGPASSWORD}" ]]; then
    echo "SML_PGPASSWORD is required when SML_IMAGE_DATABASE_URL is not set" >&2
    exit 1
  fi

  IMAGE_DB="$(printf '%s' "${SML_TENANT}" | tr '[:upper:]' '[:lower:]')_images"
fi

run_psql() {
  if [[ "${USE_DATABASE_URL}" == "true" ]]; then
    "${PSQL_CMD[@]}" "${DATABASE_URL}" -v ON_ERROR_STOP=1 "$@"
    return
  fi

  PGPASSWORD="${SML_PGPASSWORD}" "${PSQL_CMD[@]}" \
    -h "${SML_PGHOST}" \
    -p "${SML_PGPORT}" \
    -U "${SML_PGUSER}" \
    -d "${IMAGE_DB}" \
    -v ON_ERROR_STOP=1 \
    "$@"
}

echo "Applying SML image index..."
run_psql < "${SQL_FILE}"

echo "Verifying index..."
run_psql -c \
  "SELECT indexname FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'images' AND indexname = 'images_trim_image_id_order_roworder_file_idx';"

echo "Done."

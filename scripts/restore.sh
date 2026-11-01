#!/usr/bin/env bash
# 가계부 DB 복구 (backup.sh로 만든 .dump 파일).
#
# 사용:
#   TARGET_DATABASE_URL=postgres://.../myhome_restore scripts/restore.sh backups/myhome-20261101-120000.dump --yes
#
# 주의: 대상 DB의 같은 이름 테이블을 지우고(--clean) 백업 내용으로 바꾼다.
#       먼저 빈 새 DB에 복구해서 확인한 뒤 서버의 DATABASE_URL을 바꾸는 방법을 권장한다(docs/BACKUP.md).
set -euo pipefail

FILE="${1:-}"
CONFIRM="${2:-}"
: "${TARGET_DATABASE_URL:?TARGET_DATABASE_URL(복구할 대상 DB)이 필요합니다}"
if [[ -z "$FILE" || ! -f "$FILE" ]]; then
  echo "사용: TARGET_DATABASE_URL=... $0 <백업파일.dump> --yes" >&2
  exit 2
fi
if [[ "$CONFIRM" != "--yes" ]]; then
  echo "대상 DB의 기존 테이블을 백업 내용으로 덮어씁니다."
  echo "  파일: $FILE"
  echo "  대상: ${TARGET_DATABASE_URL%%\?*}" | sed -E 's#://[^@]*@#://***@#'
  echo "계속하려면 마지막에 --yes를 붙여 다시 실행하세요."
  exit 1
fi

pg_restore --clean --if-exists --no-owner --no-privileges --exit-on-error --dbname="$TARGET_DATABASE_URL" "$FILE"
echo "복구 완료. 확인:"
psql "$TARGET_DATABASE_URL" -At -c "SELECT 'raw_events', count(*) FROM raw_events UNION ALL SELECT 'transactions', count(*) FROM transactions UNION ALL SELECT 'goose_version', max(version_id) FROM goose_db_version"

#!/usr/bin/env bash
# 가계부 DB 백업 (pg_dump custom 형식, 압축 포함).
#
# 사용:
#   DATABASE_URL=postgres://... scripts/backup.sh [출력 폴더]
#   scripts/backup.sh --docker [출력 폴더]      # 로컬 docker compose의 postgres 컨테이너에서 백업
#
# 환경변수:
#   DATABASE_URL  백업할 DB (--docker가 아니면 필수)
#   BACKUP_DIR    출력 폴더 기본값 (기본: ./backups)
#   KEEP_DAYS     설정하면 이보다 오래된 백업 파일을 지운다. 비우면 아무것도 지우지 않는다(기본).
#
# 백업 파일에는 알림 원문 등 개인정보가 들어 있다. 저장소에 커밋하지 말고(.gitignore에 있음) 안전한 곳에 보관한다.
set -euo pipefail
umask 077

USE_DOCKER=0
if [[ "${1:-}" == "--docker" ]]; then
  USE_DOCKER=1
  shift
fi
OUT_DIR="${1:-${BACKUP_DIR:-./backups}}"
mkdir -p "$OUT_DIR"

TS="$(TZ=Asia/Seoul date +%Y%m%d-%H%M%S)"
FILE="$OUT_DIR/myhome-$TS.dump"
TMP="$FILE.partial"

if [[ $USE_DOCKER -eq 1 ]]; then
  docker compose exec -T postgres pg_dump --format=custom --no-owner --no-privileges -U myhome -d myhome > "$TMP"
else
  : "${DATABASE_URL:?DATABASE_URL이 필요합니다 (또는 --docker)}"
  pg_dump --format=custom --no-owner --no-privileges --dbname="$DATABASE_URL" --file="$TMP"
fi

# 백업 파일이 읽히는지 확인(목차를 읽어 본다)
if command -v pg_restore >/dev/null 2>&1; then
  pg_restore --list "$TMP" >/dev/null
elif [[ $USE_DOCKER -eq 1 ]]; then
  docker compose exec -T postgres pg_restore --list < "$TMP" >/dev/null
fi
mv "$TMP" "$FILE"
echo "백업 완료: $FILE ($(du -h "$FILE" | cut -f1))"

if [[ -n "${KEEP_DAYS:-}" ]]; then
  echo "${KEEP_DAYS}일보다 오래된 백업 정리:"
  find "$OUT_DIR" -maxdepth 1 -name 'myhome-*.dump' -type f -mtime +"$KEEP_DAYS" -print -delete
fi

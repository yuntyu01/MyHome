#!/usr/bin/env bash
# myhome VM 백업 (VM에서 root로, myhome-backup.timer가 매일 부른다). deploy.sh가 /opt/myhome/backup.sh로 설치한다.
#
#   1. pg_dump(custom 형식)를 /var/backups/myhome/db/에 만들고 pg_restore --list로 읽히는지 확인
#   2. BACKUP_PAR_URL(쓰기 전용 PAR, /etc/myhome/backup.env)이 있으면 OCI Object Storage에 올린다
#        db/myhome-YYYYMMDD-HHMMSS.dump   매번 (버킷 수명 주기 규칙이 60일 뒤 지운다)
#        photos/<날짜>/<파일>              끼니 사진은 한 장에 한 번만 (올린 목록을 기억한다)
#   3. VM 디스크에는 덤프를 LOCAL_KEEP_DAYS(기본 14)일만 남긴다. 사진은 지우지 않는다(원본이다)
#
# 실패하면 0이 아닌 코드로 끝나고, 유닛의 OnFailure=가 앱 알림(invest_alerts)을 남긴다.
# 복구: docs/BACKUP.md 「VM」
set -euo pipefail
umask 077

ROOT=/var/backups/myhome
PHOTOS=${MEAL_PHOTO_DIR:-/var/lib/myhome/meal_photos}
KEEP=${LOCAL_KEEP_DAYS:-14}
set -a
. /etc/myhome/owner.env               # DATABASE_URL = 스키마 소유자(전체 읽기)
[ -f /etc/myhome/backup.env ] && . /etc/myhome/backup.env
set +a

install -d -m 0700 "$ROOT" "$ROOT/db"
TS=$(TZ=Asia/Seoul date +%Y%m%d-%H%M%S)
NAME="myhome-$TS.dump"
TMP="$ROOT/db/$NAME.partial"
pg_dump --format=custom --no-owner --no-privileges --dbname="$DATABASE_URL" --file="$TMP"
pg_restore --list "$TMP" >/dev/null
mv "$TMP" "$ROOT/db/$NAME"
echo "덤프: $NAME ($(du -h "$ROOT/db/$NAME" | cut -f1))"

put() { # put <로컬 파일> <객체 이름>
  curl -fsS --retry 3 --retry-delay 5 -m 300 -X PUT --data-binary @"$1" \
    -H 'Content-Type: application/octet-stream' "${BACKUP_PAR_URL%/}/$2" -o /dev/null
}

if [ -n "${BACKUP_PAR_URL:-}" ]; then
  put "$ROOT/db/$NAME" "db/$NAME"
  echo "올림: db/$NAME"
  if [ -d "$PHOTOS" ]; then
    DONE="$ROOT/photos-uploaded.txt"; touch "$DONE"
    n=0
    while IFS= read -r -d '' f; do
      rel=${f#"$PHOTOS"/}
      grep -qxF "$rel" "$DONE" && continue
      put "$f" "photos/$rel"
      echo "$rel" >> "$DONE"; n=$((n + 1))
    done < <(find "$PHOTOS" -type f -name '*.jpg' -print0 | sort -z)
    echo "사진 새로 올림: ${n}장"
  fi
else
  echo "⚠ BACKUP_PAR_URL 없음: VM 디스크에만 백업했다 (docs/BACKUP.md 「VM」)"
fi

find "$ROOT/db" -maxdepth 1 -name 'myhome-*.dump' -type f -mtime +"$KEEP" -print -delete
find "$ROOT/db" -maxdepth 1 -name '*.partial' -type f -mmin +60 -delete

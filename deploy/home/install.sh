#!/usr/bin/env bash
# VM 안에서 root로 도는 설치 단계. 배포 묶음(바이너리 myhome + systemd/ + 스크립트)을 푼 디렉터리에서 실행한다.
#
#   deploy.sh(사람, PC에서)   → 비밀값을 보낸 뒤 이 스크립트를 부른다. 터널 설정도 맞춘다(TUNNEL_ID 등)
#   ci-deploy.sh(CI, push마다) → 묶음만 받아 이 스크립트를 부른다. 비밀값·터널은 건드리지 않는다
#
# 비밀값(/etc/myhome/*.app.env)은 이미 VM에 있어야 한다. 이 스크립트는 그것을 합치기만 한다.
set -euo pipefail
cd "$(dirname "$0")"
[ "$(id -u)" -eq 0 ] || { echo "✗ root로 실행할 것" >&2; exit 1; }
for f in myhome systemd postgres-roles.sql backup.sh enable-invest.sh disable-invest.sh ci-deploy.sh; do
  [ -e "$f" ] || { echo "✗ 배포 묶음에 $f 없음" >&2; exit 1; }
done

# 환경 파일 합치기 (DB 주소 + 앱 설정)
for who in web trader; do
  [ -s /etc/myhome/$who.app.env ] || { echo "✗ /etc/myhome/$who.app.env 없음 - deploy.sh를 사람이 한 번 돌릴 것" >&2; exit 1; }
  cat /etc/myhome/$who.db.env /etc/myhome/$who.app.env > /etc/myhome/$who.env
  chown root:myhome-$who /etc/myhome/$who.env; chmod 0640 /etc/myhome/$who.env
done

# 바꿔치기: 실행 중인 작업(매매 timer 포함)은 옛 바이너리로 끝까지 돈다
install -m 0755 myhome /opt/myhome/myhome.new
mv -f /opt/myhome/myhome.new /opt/myhome/myhome
# 무엇이 깔렸는지(커밋) 남긴다
if [ -f VERSION ]; then install -m 0644 VERSION /opt/myhome/VERSION; fi

# 마이그레이션은 소유자 역할로, 그 뒤 권한 다시 걸기 (멱등)
set -a; . /etc/myhome/web.app.env; . /etc/myhome/owner.env; set +a
/opt/myhome/myhome migrate up
psql "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -f postgres-roles.sql

# systemd 유닛: 웹과 백업을 켠다. 🔴 매매 타이머는 켜고 끄는 것을 사람이 한다(enable/disable-invest.sh) -
# 여기서는 유닛 파일만 바꾸고 켜짐 상태는 그대로 둔다
install -m 0755 backup.sh /opt/myhome/backup.sh
install -m 0755 enable-invest.sh disable-invest.sh ci-deploy.sh /opt/myhome/
cp systemd/*.service systemd/*.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now myhome.service myhome-backup.timer >/dev/null
systemctl restart myhome.service

# 터널: 들어오는 포트 없이 Cloudflare로 나가는 연결만. TUNNEL_ID가 있을 때만(사람의 deploy.sh) 맞춘다.
# SSH도 이 터널을 타므로, 설정이 바뀌었을 때만 그리고 맨 끝에 미뤄서 다시 시작한다(지금 SSH가 끊겨도 배포가 끝나게)
RESTART_TUNNEL=""
if [ -n "${TUNNEL_ID:-}" ]; then
  NEWCFG=$(mktemp)
  cat > "$NEWCFG" <<EOF
tunnel: $TUNNEL_ID
credentials-file: /etc/cloudflared/$TUNNEL_ID.json
ingress:
  - hostname: $HOSTNAME_PUBLIC
    service: http://127.0.0.1:8080
  - hostname: $HOSTNAME_SSH
    service: ssh://127.0.0.1:22
  - service: http_status:404
EOF
  if ! cmp -s "$NEWCFG" /etc/cloudflared/config.yml; then
    install -m 0644 "$NEWCFG" /etc/cloudflared/config.yml
    RESTART_TUNNEL=1
  fi
  rm -f "$NEWCFG"
  if [ ! -f /etc/systemd/system/cloudflared.service ]; then
    cloudflared --config /etc/cloudflared/config.yml service install >/dev/null 2>&1
    RESTART_TUNNEL=""
  fi
fi

code=000
for i in $(seq 1 20); do
  code=$(curl -s -m 2 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/healthz || true)
  [ "$code" = 200 ] && break
  sleep 1
done
echo "웹: $(systemctl is-active myhome) / healthz: $code / 터널: $(systemctl is-active cloudflared)"
echo "백업: 다음 $(systemctl show myhome-backup.timer -p NextElapseUSecRealtime --value) / 버킷 $( [ -s /etc/myhome/backup.env ] && echo 연결됨 || echo '없음(VM 디스크만)')"
echo "매매 타이머: $(systemctl list-unit-files 'myhome-invest-*.timer' --no-legend | awk '{print $1"="$2}' | tr '\n' ' ')"
echo "버전: $(cat /opt/myhome/VERSION 2>/dev/null || echo '?')"
if [ -n "$RESTART_TUNNEL" ]; then
  systemd-run --quiet --on-active=3 --unit="myhome-tunnel-restart-$(date +%s)" systemctl restart cloudflared
  echo "터널 설정이 바뀌어 3초 뒤 cloudflared를 다시 시작한다(SSH가 잠깐 끊긴다)"
fi
[ "$code" = 200 ] || { echo "✗ healthz $code - 웹이 안 떴다. journalctl -u myhome 확인" >&2; exit 1; }

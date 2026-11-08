#!/usr/bin/env bash
# myhome VM 배포 (로컬에서 실행). setup-vm.sh를 먼저 한 번 돌려 둔다.
#
#   deploy/home/deploy.sh            # 바이너리 빌드 → 마이그레이션 → 권한 → 웹 재시작 → 터널
#
# 비밀값은 저장소 밖 $MYHOME_SECRETS(기본 /root/myhome-secrets)에 둔다:
#   web.env     웹 앱 설정(Access, Gemini …)        → VM /etc/myhome/web.app.env
#   trader.env  토스 키                              → VM /etc/myhome/trader.app.env
#   tunnel.json cloudflared 터널 인증(선택, 없으면 ~/.cloudflared/$TUNNEL_ID.json)
# 모두 ssh 표준입력으로 보내서 명령줄·로그에 남지 않는다.
#
# 🔴 자동매매 타이머(myhome-invest-*.timer)는 설치만 하고 켜지 않는다. 켜는 건 사람이 한다
#    (TQ에서 넘어오는 날을 정하고 import-tq를 한 다음): systemctl enable --now myhome-invest-*.timer
set -euo pipefail

HOST=${MYHOME_HOST:-home}
SECRETS=${MYHOME_SECRETS:-/root/myhome-secrets}
TUNNEL_ID=${MYHOME_TUNNEL_ID:-280ce9be-1905-4682-a380-cad5682e50d9}
HOSTNAME_PUBLIC=${MYHOME_HOSTNAME:-myhome.kodeploy.com}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)

echo "▶ 빌드 (linux/amd64)"
BIN=$(mktemp -d)/myhome
(cd "$ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$BIN" ./cmd/myhome)

echo "▶ 비밀값·설정 전송"
for who in web trader; do
  [ -f "$SECRETS/$who.env" ] || { echo "✗ $SECRETS/$who.env 없음" >&2; exit 1; }
  ssh "$HOST" "sudo install -m 0600 /dev/stdin /etc/myhome/$who.app.env" < "$SECRETS/$who.env"
done
CRED="$SECRETS/tunnel.json"; [ -f "$CRED" ] || CRED="$HOME/.cloudflared/$TUNNEL_ID.json"
ssh "$HOST" "sudo install -d -m 0755 /etc/cloudflared && sudo install -m 0600 /dev/stdin /etc/cloudflared/$TUNNEL_ID.json" < "$CRED"

echo "▶ 바이너리·유닛 전송"
ssh "$HOST" "sudo install -m 0755 /dev/stdin /opt/myhome/myhome.new" < "$BIN"
tar -C "$ROOT/deploy/home" -cf - systemd postgres-roles.sql | ssh "$HOST" "rm -rf /tmp/myhome-deploy && mkdir -p /tmp/myhome-deploy && tar -C /tmp/myhome-deploy -xf -"

ssh "$HOST" "sudo TUNNEL_ID=$TUNNEL_ID HOSTNAME_PUBLIC=$HOSTNAME_PUBLIC bash -s" <<'REMOTE'
set -euo pipefail
cd /tmp/myhome-deploy
# 환경 파일 합치기 (DB 주소 + 앱 설정)
for who in web trader; do
  cat /etc/myhome/$who.db.env /etc/myhome/$who.app.env > /etc/myhome/$who.env
  chown root:myhome-$who /etc/myhome/$who.env; chmod 0640 /etc/myhome/$who.env
done
# 바꿔치기: 실행 중인 작업은 옛 바이너리로 끝까지 돈다
mv -f /opt/myhome/myhome.new /opt/myhome/myhome

# 마이그레이션은 소유자 역할로, 그 뒤 권한 다시 걸기 (멱등)
set -a; . /etc/myhome/web.app.env; . /etc/myhome/owner.env; set +a
/opt/myhome/myhome migrate up
psql "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -f postgres-roles.sql

# systemd 유닛: 웹만 켠다. 매매 타이머는 설치만
cp systemd/*.service systemd/*.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now myhome.service >/dev/null
systemctl restart myhome.service

# 터널: 들어오는 포트 없이 Cloudflare로 나가는 연결만
cat > /etc/cloudflared/config.yml <<EOF
tunnel: $TUNNEL_ID
credentials-file: /etc/cloudflared/$TUNNEL_ID.json
ingress:
  - hostname: $HOSTNAME_PUBLIC
    service: http://127.0.0.1:8080
  - service: http_status:404
EOF
if [ ! -f /etc/systemd/system/cloudflared.service ]; then
  cloudflared --config /etc/cloudflared/config.yml service install >/dev/null 2>&1
fi
systemctl restart cloudflared

for i in $(seq 1 20); do curl -fsS -m 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1 && break; sleep 1; done
echo "웹: $(systemctl is-active myhome) / healthz: $(curl -s -m 3 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/healthz) / 터널: $(systemctl is-active cloudflared)"
echo "매매 타이머(꺼져 있어야 정상): $(systemctl list-unit-files 'myhome-invest-*.timer' --no-legend | awk '{print $1"="$2}' | tr '\n' ' ')"
rm -rf /tmp/myhome-deploy
REMOTE
echo "✓ 배포 끝"

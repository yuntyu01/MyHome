#!/usr/bin/env bash
# myhome VM 배포 (로컬에서 실행). setup-vm.sh를 먼저 한 번 돌려 둔다.
#
#   deploy/home/deploy.sh            # 바이너리 빌드 → 비밀값 → 마이그레이션 → 권한 → 웹 재시작 → 터널
#
# 평소 배포는 CI가 한다(main에 push → 테스트 → .github/workflows/server.yml → ci-deploy.sh).
# 이 스크립트는 비밀값·터널을 바꿀 때, CI가 막혔을 때 쓴다. 설치 단계는 둘이 같다(install.sh).
# CI 키를 VM에 까는 것은 setup-ci-deploy.sh(한 번, 사람이).
#
# 비밀값은 저장소 밖 $MYHOME_SECRETS(기본 /root/myhome-secrets)에 둔다:
#   web.env     웹 앱 설정(Access, Gemini …)        → VM /etc/myhome/web.app.env
#   trader.env  토스 키                              → VM /etc/myhome/trader.app.env
#   tunnel.json cloudflared 터널 인증(선택, 없으면 ~/.cloudflared/$TUNNEL_ID.json)
# 모두 ssh 표준입력으로 보내서 명령줄·로그에 남지 않는다.
#
# 🔴 자동매매 타이머(myhome-invest-*.timer)는 설치만 하고 켜지 않는다. 켜는 건 사람이 한다
#    (TQ에서 넘어오는 날을 정하고 import-tq를 한 다음): VM에서 sudo /opt/myhome/enable-invest.sh
#    (끄기는 disable-invest.sh). systemctl enable --now를 바로 쓰면 지난 실행 기록 때문에 켜자마자 돌 수 있다
set -euo pipefail

HOST=${MYHOME_HOST:-home}
SECRETS=${MYHOME_SECRETS:-/root/myhome-secrets}
TUNNEL_ID=${MYHOME_TUNNEL_ID:-280ce9be-1905-4682-a380-cad5682e50d9}
HOSTNAME_PUBLIC=${MYHOME_HOSTNAME:-myhome.kodeploy.com}
# SSH도 같은 터널로 받는다(들어오는 포트 없음, KoDeploy 관리 노드를 거치지 않음). PC는 cloudflared access ssh로 붙는다
HOSTNAME_SSH=${MYHOME_SSH_HOSTNAME:-myhome-ssh.kodeploy.com}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)

echo "▶ 빌드 (linux/amd64)"
BIN=$(mktemp -d)/myhome
(cd "$ROOT/server" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$BIN" ./cmd/myhome)

echo "▶ 비밀값·설정 전송"
for who in web trader; do
  [ -f "$SECRETS/$who.env" ] || { echo "✗ $SECRETS/$who.env 없음" >&2; exit 1; }
  ssh "$HOST" "sudo install -m 0600 /dev/stdin /etc/myhome/$who.app.env" < "$SECRETS/$who.env"
done
# 백업 버킷 쓰기 전용 주소(선택). 없으면 VM 디스크에만 백업한다 - docs/BACKUP.md 「VM」
if [ -f "$SECRETS/backup.env" ]; then
  ssh "$HOST" "sudo install -m 0600 /dev/stdin /etc/myhome/backup.env" < "$SECRETS/backup.env"
fi
CRED="$SECRETS/tunnel.json"; [ -f "$CRED" ] || CRED="$HOME/.cloudflared/$TUNNEL_ID.json"
ssh "$HOST" "sudo install -d -m 0755 /etc/cloudflared && sudo install -m 0600 /dev/stdin /etc/cloudflared/$TUNNEL_ID.json" < "$CRED"

echo "▶ 배포 묶음 전송·설치"
STAGE=$(mktemp -d)
cp "$BIN" "$STAGE/myhome"
cp -r "$ROOT/deploy/home/systemd" "$STAGE/"
cp "$ROOT/deploy/home/"{postgres-roles.sql,backup.sh,enable-invest.sh,disable-invest.sh,install.sh,ci-deploy.sh} "$STAGE/"
(cd "$ROOT" && echo "$(git rev-parse --short HEAD)$(git diff --quiet HEAD -- server deploy || echo '+로컬수정') (사람 deploy.sh)") > "$STAGE/VERSION"
tar -C "$STAGE" -cf - . | ssh "$HOST" "rm -rf /tmp/myhome-deploy && mkdir -p /tmp/myhome-deploy && tar -C /tmp/myhome-deploy -xf -"
rm -rf "$STAGE"

ssh "$HOST" "sudo TUNNEL_ID=$TUNNEL_ID HOSTNAME_PUBLIC=$HOSTNAME_PUBLIC HOSTNAME_SSH=$HOSTNAME_SSH bash /tmp/myhome-deploy/install.sh; rc=\$?; rm -rf /tmp/myhome-deploy; exit \$rc"
echo "✓ 배포 끝"

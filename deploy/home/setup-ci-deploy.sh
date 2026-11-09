#!/usr/bin/env bash
# CI 자동 배포 키를 VM에 한 번 깐다 (PC에서 사람이 실행). 다시 돌려도 같다(멱등).
#
#   deploy/home/setup-ci-deploy.sh
#
# VM에 myhome-deploy 사용자를 만들고 deploy/home/ci-deploy.pub을 authorized_keys에 넣는다.
# 그 키는 restrict,command=로 묶여 `sudo /opt/myhome/ci-deploy.sh` 하나만 실행한다(셸·포워딩 없음).
# sudo도 그 스크립트 하나만 허용한다. 개인키는 GitHub Secret HOME_DEPLOY_KEY에만 둔다.
#
# 끊기: ssh home "sudo rm /home/myhome-deploy/.ssh/authorized_keys"
set -euo pipefail
HOST=${MYHOME_HOST:-home}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PUB="$ROOT/deploy/home/ci-deploy.pub"
[ -s "$PUB" ] || { echo "✗ $PUB 없음" >&2; exit 1; }
grep -q '^ssh-ed25519 ' "$PUB" || { echo "✗ $PUB 이 ed25519 공개키가 아니다" >&2; exit 1; }

ssh "$HOST" "sudo bash -s" <<REMOTE
set -euo pipefail
[ -x /opt/myhome/ci-deploy.sh ] || { echo "✗ /opt/myhome/ci-deploy.sh 없음 - deploy.sh를 먼저 돌릴 것" >&2; exit 1; }
id myhome-deploy >/dev/null 2>&1 || useradd --system --create-home --shell /bin/bash myhome-deploy
install -d -o myhome-deploy -g myhome-deploy -m 0700 /home/myhome-deploy/.ssh
printf 'restrict,command="sudo /opt/myhome/ci-deploy.sh" %s\n' "$(cat "$PUB")" > /home/myhome-deploy/.ssh/authorized_keys
chown myhome-deploy:myhome-deploy /home/myhome-deploy/.ssh/authorized_keys
chmod 0600 /home/myhome-deploy/.ssh/authorized_keys
echo 'myhome-deploy ALL=(root) NOPASSWD: /opt/myhome/ci-deploy.sh' > /etc/sudoers.d/myhome-deploy
chmod 0440 /etc/sudoers.d/myhome-deploy
visudo -cf /etc/sudoers.d/myhome-deploy >/dev/null
echo "✓ myhome-deploy 준비됨 (키: \$(ssh-keygen -lf /home/myhome-deploy/.ssh/authorized_keys | awk '{print \$2}'))"
REMOTE

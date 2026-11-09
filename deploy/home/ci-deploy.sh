#!/usr/bin/env bash
# CI 배포 받는 쪽. VM의 /opt/myhome/ci-deploy.sh (root 소유, install.sh가 깐다).
#
# CI 키(myhome-deploy 사용자의 authorized_keys)는 이 명령만 실행할 수 있다(command=, restrict):
#   ssh myhome-deploy@VM < bundle.tar.gz   →   sudo /opt/myhome/ci-deploy.sh
# 셸·포트 포워딩은 없다. 표준입력으로 받은 배포 묶음을 풀어 install.sh를 부른다.
# 비밀값·터널 설정은 건드리지 않는다 - 그것은 사람이 deploy.sh로 한다.
#
# 🔴 이 키가 새면 main에 push할 수 있는 것과 같다(배포 = root에서 마이그레이션 실행). GitHub Secret에만 둔다.
#    끊으려면 VM에서: sudo rm /home/myhome-deploy/.ssh/authorized_keys
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "✗ sudo로 실행된다" >&2; exit 1; }
exec 9>/run/myhome-ci-deploy.lock
flock -w 600 9 || { echo "✗ 다른 배포가 10분 넘게 진행 중" >&2; exit 75; }

DIR=$(mktemp -d /var/tmp/myhome-ci.XXXXXX)
trap 'rm -rf "$DIR"' EXIT
# 묶음은 100MB까지만 받는다(바이너리 ~30MB)
head -c 100000000 | tar -xz -C "$DIR" --no-same-owner --no-same-permissions
[ -f "$DIR/install.sh" ] || { echo "✗ 묶음에 install.sh 없음" >&2; exit 1; }
echo "▶ CI 배포 $(cat "$DIR/VERSION" 2>/dev/null || echo '?') ($(date '+%F %T %Z'))"
bash "$DIR/install.sh" 2>&1 | tee -a /var/log/myhome-ci-deploy.log
exit "${PIPESTATUS[0]}"

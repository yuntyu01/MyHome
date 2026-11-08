#!/usr/bin/env bash
# 자동매매 타이머 켜기. VM에서 root로, 사람이 실행한다 (deploy.sh가 /opt/myhome/에 설치한다).
#
#   sudo /opt/myhome/enable-invest.sh
#
# 켜기 전에: import-tq로 TQ 상태를 옮겼고, TQ 쪽 실행을 멈췄는지 확인한다(토스 토큰이 1개라 동시 실행 금지).
# 이 스크립트는 주문을 내지 않는다. 마지막에 status(읽기 전용 점검)를 한 번 돌려 /healthz/invest를 채운다.
#
# 🔴 타이머는 Persistent=true다. systemd는 지난 실행 시각(/var/lib/systemd/timers/stamp-<타이머>)을 보고,
#    꺼져 있던 동안 놓친 일정이 있으면 켜는 즉시 돌린다. 예전에 켰다 끈 흔적이 있으면 enable 순간 trade가
#    돌 수 있다. 그래서 켜기 전에 그 기록을 지운다 — 기록이 없으면 다음 일정까지 기다린다.
set -euo pipefail

TIMERS=(myhome-invest-refresh.timer myhome-invest-judge.timer myhome-invest-trade.timer myhome-invest-status.timer)
STAMPS=/var/lib/systemd/timers

[ "$(id -u)" -eq 0 ] || { echo "✗ root로 실행할 것 (sudo)" >&2; exit 1; }
for t in "${TIMERS[@]}"; do
  [ -f "/etc/systemd/system/$t" ] || { echo "✗ /etc/systemd/system/$t 없음 - deploy.sh를 먼저 돌릴 것" >&2; exit 1; }
done
[ -s /etc/myhome/trader.env ] || { echo "✗ /etc/myhome/trader.env 없음 - deploy.sh를 먼저 돌릴 것" >&2; exit 1; }
systemctl daemon-reload

echo "▶ 지난 실행 기록 확인"
for t in "${TIMERS[@]}"; do
  if systemctl is-active --quiet "$t"; then
    echo "  $t 이미 켜져 있다 - 그대로 둔다"
    continue
  fi
  stamp="$STAMPS/stamp-$t"
  if [ -e "$stamp" ]; then
    echo "  $t 지난 실행 기록 $(date -r "$stamp" '+%F %T %Z') - 지운다 (켜자마자 놓친 일정을 돌리지 않게)"
    rm -f "$stamp"
  else
    echo "  $t 기록 없음"
  fi
done

echo "▶ 타이머 켜기"
for t in "${TIMERS[@]}"; do
  systemctl enable --now "$t" >/dev/null
done

# 켜는 순간 돌기 시작한 작업이 있으면 알린다(있으면 안 된다)
for t in "${TIMERS[@]}"; do
  svc="${t%.timer}.service"
  state=$(systemctl is-active "$svc" || true)
  if [ "$state" = "active" ] || [ "$state" = "activating" ]; then
    echo "⚠ $svc 가 지금 돌고 있다($state). 의도한 것이 아니면: journalctl -u $svc -f" >&2
  fi
done

echo "▶ 첫 점검(status, 읽기 전용) - 성공하면 /healthz/invest가 정상이 된다"
if systemctl start myhome-invest-status.service; then
  echo "  status 성공"
else
  echo "⚠ status 실패 - 앱 알림이 간다. journalctl -u myhome-invest-status -n 50" >&2
fi

echo "▶ 다음 실행 일정"
systemctl list-timers --all "${TIMERS[@]}"

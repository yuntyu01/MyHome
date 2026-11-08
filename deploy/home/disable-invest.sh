#!/usr/bin/env bash
# 자동매매 타이머 끄기. VM에서 root로, 사람이 실행한다 (deploy.sh가 /opt/myhome/에 설치한다).
#
#   sudo /opt/myhome/disable-invest.sh
#
# 타이머만 끈다. 지금 돌고 있는 작업은 끊지 않는다 - 주문을 보내는 도중에 죽이면 결과 모름 주문이 남는다
# (다음 trade가 증권사에서 찾아 정리하지만, 끄는 이유가 그 trade를 안 돌리려는 것일 수 있다).
# 다시 켤 때는 enable-invest.sh를 쓴다(놓친 일정을 켜자마자 돌리지 않게 실행 기록을 정리한다).
set -euo pipefail

TIMERS=(myhome-invest-refresh.timer myhome-invest-judge.timer myhome-invest-trade.timer myhome-invest-status.timer)

[ "$(id -u)" -eq 0 ] || { echo "✗ root로 실행할 것 (sudo)" >&2; exit 1; }

echo "▶ 타이머 끄기"
systemctl disable --now "${TIMERS[@]}"

for t in "${TIMERS[@]}"; do
  svc="${t%.timer}.service"
  state=$(systemctl is-active "$svc" || true)
  if [ "$state" = "active" ] || [ "$state" = "activating" ]; then
    echo "⚠ $svc 가 아직 돌고 있다($state). 끝날 때까지 둔다: journalctl -u $svc -f" >&2
  fi
done

echo "▶ 상태 (전부 disabled·inactive여야 한다)"
systemctl list-unit-files --no-legend "${TIMERS[@]}"
systemctl list-timers --all "${TIMERS[@]}"

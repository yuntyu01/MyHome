#!/usr/bin/env bash
# myhome VM 첫 설정 (Ubuntu 24.04 Minimal, OCI E2.1.Micro 1GB). VM에서 root로 돈다. 여러 번 돌려도 된다.
#
#   ssh home 'sudo bash -s' < deploy/home/setup-vm.sh
#
# 하는 일: 스왑 2GB, PostgreSQL(작게), cloudflared, OS 사용자(myhome-web, myhome-trader),
# DB 역할 3개(owner/web/trader)와 /etc/myhome/*.env 뼈대. 비밀번호는 VM에서 만들고 밖으로 내보내지 않는다.
# 앱 비밀값(Gemini, 토스, Access)은 deploy.sh가 표준입력으로 넣는다.
set -euo pipefail

# --- 스왑: 1GB 메모리에 PostgreSQL + 웹 + 매매 작업이 같이 돈다 ---
if ! swapon --show | grep -q /swapfile; then
  fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
  grep -q '^/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  sysctl -w vm.swappiness=10 && echo 'vm.swappiness=10' > /etc/sysctl.d/90-myhome.conf
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q postgresql ca-certificates curl gnupg unattended-upgrades

# --- cloudflared (Cloudflare 공식 apt 저장소) ---
if ! command -v cloudflared >/dev/null; then
  install -d -m 0755 /usr/share/keyrings
  curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg -o /usr/share/keyrings/cloudflare-main.gpg
  echo 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared noble main' \
    > /etc/apt/sources.list.d/cloudflared.list
  apt-get update -q && apt-get install -y -q cloudflared
fi

# --- 자동 보안 업데이트는 켜되, 재부팅은 매매 시각(매월 초 00:17 KST, 판정 창)과 겹치지 않게 일요일 낮에만 ---
cat > /etc/apt/apt.conf.d/52myhome-reboot <<'EOF'
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "13:00";
EOF
timedatectl set-timezone Asia/Seoul

# --- PostgreSQL: 작게 ---
PGV=$(ls /etc/postgresql | sort -V | tail -1)
cat > /etc/postgresql/$PGV/main/conf.d/myhome.conf <<'EOF'
listen_addresses = 'localhost'
max_connections = 30
shared_buffers = 64MB
work_mem = 2MB
maintenance_work_mem = 32MB
effective_cache_size = 256MB
EOF
systemctl restart postgresql

# --- OS 사용자 ---
id myhome-web >/dev/null 2>&1 || useradd --system --home /var/lib/myhome-web --shell /usr/sbin/nologin myhome-web
id myhome-trader >/dev/null 2>&1 || useradd --system --home /var/lib/myhome-trader --shell /usr/sbin/nologin myhome-trader
install -d -m 0755 /opt/myhome
install -d -m 0750 -o root -g root /etc/myhome

# --- DB 역할과 비밀번호 (처음 한 번만 만든다) ---
# tr </dev/urandom | head 은 SIGPIPE로 pipefail에 걸린다 — 정해진 양만 읽는다
pw() { head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | cut -c1-32; }
dburl() { echo "postgres://$1:$2@127.0.0.1:5432/myhome?sslmode=disable"; }
if [ ! -f /etc/myhome/db-roles.done ]; then
  OWN=$(pw); WEB=$(pw); TRD=$(pw)
  sudo -u postgres psql -v ON_ERROR_STOP=1 -q <<EOF
DO \$\$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='myhome_owner') THEN CREATE ROLE myhome_owner LOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='myhome_web') THEN CREATE ROLE myhome_web LOGIN; END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='myhome_trader') THEN CREATE ROLE myhome_trader LOGIN; END IF;
END \$\$;
ALTER ROLE myhome_owner PASSWORD '$OWN';
ALTER ROLE myhome_web PASSWORD '$WEB';
ALTER ROLE myhome_trader PASSWORD '$TRD';
EOF
  sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='myhome'" | grep -q 1 \
    || sudo -u postgres createdb -O myhome_owner myhome
  umask 077
  echo "DATABASE_URL=$(dburl myhome_owner "$OWN")" > /etc/myhome/owner.env
  echo "DATABASE_URL=$(dburl myhome_web "$WEB")" > /etc/myhome/web.db.env
  echo "DATABASE_URL=$(dburl myhome_trader "$TRD")" > /etc/myhome/trader.db.env
  touch /etc/myhome/db-roles.done
fi

# web.env / trader.env = DB 주소 + deploy.sh가 넣는 앱 설정(*.app.env). systemd는 이 두 파일을 합친 결과를 읽는다.
for who in web trader; do
  touch /etc/myhome/$who.app.env
  cat /etc/myhome/$who.db.env /etc/myhome/$who.app.env > /etc/myhome/$who.env
  chown root:myhome-$who /etc/myhome/$who.env && chmod 0640 /etc/myhome/$who.env
done
chmod 0600 /etc/myhome/owner.env /etc/myhome/*.db.env /etc/myhome/*.app.env

echo "setup-vm 완료: postgres $PGV, cloudflared $(cloudflared --version 2>/dev/null | awk '{print $3}'), swap $(swapon --show --noheadings | awk '{print $3}')"

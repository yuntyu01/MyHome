# 백업과 복구

DB에는 알림 원문(개인정보)과 모든 거래가 들어 있다. 원문(`raw_events`)만 있으면 거래는 재처리로 다시 만들 수 있지만, **직접 고친 분류·메모·현금 입력·예산·규칙은 DB에만 있다.** 정기적으로 백업한다.

## 백업

필요한 것: PostgreSQL 클라이언트(`pg_dump`, `pg_restore`, 서버와 같은 메이저 버전 이상)

```bash
# 운영 DB
DATABASE_URL='postgres://user:pass@host:5432/myhome?sslmode=require' scripts/backup.sh ./backups

# 로컬 docker compose DB
scripts/backup.sh --docker ./backups
# 또는
make backup
```

- 결과: `backups/myhome-YYYYMMDD-HHMMSS.dump` (pg_dump custom 형식, 압축됨, 권한 600)
- 스크립트가 만든 직후 `pg_restore --list`로 파일이 읽히는지 확인한다.
- **기본은 아무것도 지우지 않는다**(데이터 삭제 금지 원칙). 그대로 매일 돌리면 디스크가 계속 차므로, 자동 백업에는 **`KEEP_DAYS=30`을 꼭 지정**한다.
- 백업 파일은 저장소에 커밋하지 않는다(`.gitignore`의 `/backups/`, `*.dump`). 외장 저장소나 암호화된 클라우드 폴더에 보관한다.

### 매일 자동 백업 (예: 서버나 PC의 cron)

```cron
# 매일 04:10 KST
10 4 * * * cd /path/to/MyHome && DATABASE_URL='...' KEEP_DAYS=30 scripts/backup.sh /secure/backups >> /var/log/myhome-backup.log 2>&1
```

kodeploy에 예약 작업 기능이 있으면 같은 명령을 등록한다(확인 필요). 관리형 DB가 자체 스냅샷을 제공하면 그것도 켜 둔다.

## 복구

**권장: 새 빈 DB에 먼저 복구해서 확인한 뒤 서버를 그쪽으로 돌린다.** 기존 DB를 덮어쓰지 않아서 안전하다.

```bash
# 1) 새 DB 만들기
createdb -h host -U user myhome_restore      # 또는 관리형 DB 콘솔에서 생성

# 2) 복구 (확인용으로 --yes 없이 한 번 실행하면 무엇을 할지 보여준다)
TARGET_DATABASE_URL='postgres://user:pass@host:5432/myhome_restore' scripts/restore.sh backups/myhome-20261101-041000.dump --yes

# 3) 스크립트가 원문·거래 건수와 마이그레이션 버전을 출력한다. 기대한 값인지 확인

# 4) 서버의 DATABASE_URL을 새 DB로 바꿔 재배포
```

- 복구 후 서버가 시작될 때 마이그레이션이 자동 적용된다(`MIGRATE_ON_START=true`). 백업이 옛 버전이어도 최신 스키마로 올라간다.
- 기존 DB에 직접 복구하면 같은 이름의 테이블을 지우고 백업 내용으로 바꾼다(`pg_restore --clean`). 그 경우에도 먼저 현재 DB를 백업해 둔다.

## 이 저장소에서 확인한 것

로컬 docker postgres 16으로 `backup.sh` → 새 DB에 `restore.sh` → 원문·거래 건수 일치까지 확인했다.

## VM (운영)

VM에서는 `myhome-backup.timer`가 매일 04:30 KST에 `/opt/myhome/backup.sh`(저장소의 `deploy/home/backup.sh`)를 돌린다. `deploy/home/deploy.sh`가 설치하고 켠다.

- DB 덤프: `/var/backups/myhome/db/myhome-YYYYMMDD-HHMMSS.dump`. VM 디스크에는 14일만 남긴다(`LOCAL_KEEP_DAYS`).
- 버킷: `/etc/myhome/backup.env`에 `BACKUP_PAR_URL`이 있으면 OCI Object Storage `myhome-backup` 버킷에도 올린다.
  - `db/<덤프 이름>`: 매일 올리고, 버킷 수명 주기 규칙이 60일 뒤 지운다.
  - `photos/<날짜>/<파일>`: 끼니 사진은 한 장에 한 번만 올린다. 올린 목록은 `/var/backups/myhome/photos-uploaded.txt`에 남는다.
- VM에는 쓰기 전용 주소(PAR)만 둔다. VM이 털려도 지난 백업을 읽거나 목록을 볼 수 없다. 덮어쓴 옛 버전은 버전 관리로 30일 남는다.
- 실패하면 `OnFailure=`가 앱에 "서버 백업 실패" 알림을 남긴다.

### 버킷 만들기 (처음 한 번)

kodeploy 저장소 `terraform/oci-infra/home_backup.tf`가 버킷, 수명 주기 규칙, 쓰기 전용 PAR(2028-11-01 만료)을 만든다. 수명 주기 규칙 때문에 테넌시에 Object Storage 서비스 정책 하나를 더한다.

```bash
cd /root/study/kodeploy/terraform/oci-infra
terraform apply -target=oci_objectstorage_bucket.home_backup -target=oci_identity_policy.home_backup_lifecycle -target=oci_objectstorage_object_lifecycle_policy.home_backup -target=oci_objectstorage_preauthrequest.home_backup_write
printf 'BACKUP_PAR_URL=%s\n' "$(terraform output -raw home_backup_par_url)" > /root/myhome-secrets/backup.env
chmod 600 /root/myhome-secrets/backup.env
```

그다음 `deploy/home/deploy.sh`를 다시 돌리면 VM에 들어간다. 바로 확인하려면 `ssh home 'sudo systemctl start myhome-backup.service; sudo journalctl -u myhome-backup -n 20'`.

### VM 백업에서 복구

```bash
# 버킷 목록과 받기 (이 PC의 OCI CLI, 관리자 키)
oci os object list -bn myhome-backup --prefix db/ --query 'data[].name' --all
oci os object get -bn myhome-backup --name db/myhome-20261109-043012.dump --file /tmp/myhome.dump
oci os object bulk-download -bn myhome-backup --prefix photos/ --download-dir /tmp/meal_photos
```

- DB: 위 「복구」처럼 새 빈 DB에 `scripts/restore.sh`로 넣고 건수를 확인한 뒤 VM의 DB로 옮긴다. VM이 살아 있으면 `/var/backups/myhome/db/`의 최근 덤프를 바로 써도 된다.
- 사진: 받은 `photos/` 아래를 VM의 `/var/lib/myhome/meal_photos/`에 같은 구조로 넣는다(소유자 `myhome-web`).

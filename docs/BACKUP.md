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

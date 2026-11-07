-- myhome VM의 PostgreSQL 역할 분리. 웹 프로세스가 뚫려도 매매 상태를 고치지 못하게 한다.
--
--   myhome_owner   스키마 소유자. 마이그레이션(`myhome migrate up`)만 이 역할로 돈다
--   myhome_web     가계부 테이블 읽기·쓰기 + invest_* 읽기만 (화면 표시용)
--   myhome_trader  invest_* 읽기·쓰기. 가계부 테이블은 못 본다
--
-- 처음 한 번 (postgres 슈퍼유저로):
--   CREATE ROLE myhome_owner LOGIN PASSWORD '…';
--   CREATE ROLE myhome_web LOGIN PASSWORD '…';
--   CREATE ROLE myhome_trader LOGIN PASSWORD '…';
--   CREATE DATABASE myhome OWNER myhome_owner;
--
-- 마이그레이션 뒤에는 매번 이 파일을 소유자로 다시 돈다 — 새 테이블에도 권한이 붙게 (멱등):
--   DATABASE_URL=<owner> myhome migrate up
--   psql "<owner>" -v ON_ERROR_STOP=1 -f deploy/home/postgres-roles.sql
--
-- web.env의 MIGRATE_ON_START는 false로 둔다 — 웹 역할에는 DDL 권한이 없다.

REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO myhome_web, myhome_trader;

DO $$
DECLARE
    t text;
BEGIN
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('REVOKE ALL ON TABLE public.%I FROM myhome_web, myhome_trader', t);
        IF t LIKE 'invest\_%' THEN
            EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.%I TO myhome_trader', t);
            EXECUTE format('GRANT SELECT ON TABLE public.%I TO myhome_web', t);
        ELSIF t <> 'goose_db_version' THEN
            EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE public.%I TO myhome_web', t);
        END IF;
    END LOOP;
    FOR t IN SELECT sequencename FROM pg_sequences WHERE schemaname = 'public' LOOP
        EXECUTE format('REVOKE ALL ON SEQUENCE public.%I FROM myhome_web, myhome_trader', t);
        IF t LIKE 'invest\_%' THEN
            EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE public.%I TO myhome_trader', t);
        ELSIF t NOT LIKE 'goose\_%' THEN
            EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE public.%I TO myhome_web', t);
        END IF;
    END LOOP;
END $$;

# 가계부 모노레포 작업 모음. `make help`로 목록을 본다.
SHELL := /bin/bash

-include .env
export

SERVER_DIR := server
TEST_DATABASE_URL ?= postgres://myhome:myhome@localhost:5432/myhome_test?sslmode=disable

.PHONY: help
help: ## 명령 목록
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

.PHONY: up
up: ## docker compose로 postgres + server 실행
	docker compose up -d --build

.PHONY: down
down: ## docker compose 중지 (데이터 볼륨은 유지)
	docker compose down

.PHONY: db
db: ## postgres만 실행
	docker compose up -d postgres

.PHONY: run
run: ## 로컬에서 서버 실행 (DATABASE_URL 필요)
	cd $(SERVER_DIR) && go run ./cmd/myhome serve

.PHONY: migrate
migrate: ## DB 마이그레이션 적용 (DATABASE_URL 필요). 예: make migrate ARGS=status
	cd $(SERVER_DIR) && go run ./cmd/myhome migrate $(or $(ARGS),up)

.PHONY: test
test: ## 서버 테스트 (TEST_DATABASE_URL이 있으면 DB 테스트 포함)
	cd $(SERVER_DIR) && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...

.PHONY: test-unit
test-unit: ## DB 없이 돌릴 수 있는 테스트만
	cd $(SERVER_DIR) && TEST_DATABASE_URL= go test ./...

.PHONY: vet
vet: ## go vet
	cd $(SERVER_DIR) && go vet ./...

.PHONY: build
build: ## 서버 바이너리 빌드 → server/bin/myhome
	cd $(SERVER_DIR) && CGO_ENABLED=0 go build -o bin/myhome ./cmd/myhome

.PHONY: backup
backup: ## 로컬 docker postgres 백업 → backups/
	scripts/backup.sh --docker ./backups

.PHONY: android-core-test
android-core-test: ## Android core(순수 Kotlin) 단위 테스트
	cd android && ./gradlew -p core test

.PHONY: docker-build
docker-build: ## 서버 Docker 이미지 빌드
	docker build -t myhome-server:local $(SERVER_DIR)

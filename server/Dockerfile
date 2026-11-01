# syntax=docker/dockerfile:1
# 멀티스테이지 빌드: Go 바이너리 하나만 담은 작은 이미지를 만든다.

FROM golang:1.26-alpine AS build
WORKDIR /src

# 회사·클라우드 프록시처럼 TLS를 재암호화하는 환경을 위한 선택 사항:
#   docker build --secret id=ca_bundle,src=/path/to/ca.crt ...
# 시크릿이 없으면 기본 인증서를 쓴다.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=ca_bundle,required=false \
    if [ -f /run/secrets/ca_bundle ]; then export SSL_CERT_FILE=/run/secrets/ca_bundle; fi; \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/myhome ./cmd/myhome

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/myhome /myhome
USER 65532:65532
EXPOSE 8080
ENV HTTP_ADDR=:8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/myhome", "healthcheck"]
ENTRYPOINT ["/myhome"]
CMD ["serve"]

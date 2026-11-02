# KoDeploy 배포용 이미지. 빌드 컨텍스트는 저장소 루트다(.dockerignore가 server/만 남긴다).
# 로컬 compose는 server/Dockerfile을 쓴다. Go 버전을 올릴 때는 두 파일을 같이 고친다.

# 컴파일은 빌더 아키텍처에서 하고 결과물만 대상 아키텍처로 만든다.
# 대상이 ARM이어도 에뮬레이션 없이 빌드된다.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/myhome ./cmd/myhome

# 셸이 없는 최소 이미지다. 플랫폼 터미널이 sh를 요구하면 scratch를 alpine:3으로 바꾼다.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/myhome /myhome
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/myhome", "healthcheck"]
ENTRYPOINT ["/myhome"]
CMD ["serve"]

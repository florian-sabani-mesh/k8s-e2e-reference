# Build from the repository root: docker build -f services/pricing/Dockerfile .
FROM golang:1.27.1-alpine3.23 AS build
WORKDIR /src
COPY pkg/ pkg/
COPY services/pricing/ services/pricing/
WORKDIR /src/services/pricing
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOWORK=off CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/server ./cmd/server
FROM alpine:3.23.3
RUN apk add --no-cache ca-certificates
COPY --from=build /out/ /app/
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/server"]

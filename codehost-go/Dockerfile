FROM golang:1.25-alpine AS toolchain
WORKDIR /src
COPY codehost-go/go.mod codehost-go/go.sum ./
RUN go mod download

FROM toolchain AS testtools
RUN apk add --no-cache git gcc musl-dev

FROM testtools AS test
COPY codehost-go/ ./
RUN test -z "$(gofmt -l .)" && go vet ./... && go test -race -coverprofile=/coverage.out ./... && go tool cover -func=/coverage.out

FROM toolchain AS builder
COPY codehost-go/ ./
RUN CGO_ENABLED=0 go build -trimpath -o /codehost ./cmd/codehost

FROM alpine:3.22
ARG CODEHOST_UID=1000
ARG CODEHOST_GID=1000
RUN apk add --no-cache git ca-certificates \
    && addgroup -g "$CODEHOST_GID" codehost \
    && adduser -D -u "$CODEHOST_UID" -G codehost codehost \
    && mkdir -p /data && chown codehost:codehost /data
WORKDIR /app
COPY --from=builder /codehost /usr/local/bin/codehost
ENV CODEHOST_GO_VALUES=/app/values.yaml CODEHOST_GO_DATA=/data CODEHOST_GO_ADDR=:8082
USER codehost
EXPOSE 8082
CMD ["codehost"]

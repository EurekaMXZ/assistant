# syntax=docker/dockerfile:1

FROM golang:1.26.4 AS builder

ARG SERVICE=backend
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org

WORKDIR /src

ENV GOPROXY=${GOPROXY}
ENV GOSUMDB=${GOSUMDB}

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY db ./db
COPY internal ./internal
COPY prompts ./prompts

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

RUN mkdir -p /out/tmp && chmod 0700 /out/tmp

FROM scratch

WORKDIR /app

ENV TMPDIR=/app/tmp

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/app /app/app
COPY --from=builder --chmod=0700 /out/tmp/ /app/tmp/
COPY --from=builder /src/db /app/db
COPY --from=builder /src/prompts /app/prompts

EXPOSE 8080

ENTRYPOINT ["/app/app"]

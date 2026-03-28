FROM golang:1.26-alpine AS builder

ARG VERSION=dev
WORKDIR /build
COPY . .
RUN go build -ldflags "-X main.Version=${VERSION}" -o /steward ./cmd/steward

FROM alpine:3.21

RUN adduser -D -u 1000 agent && mkdir -p /opt/steward/data && chown -R agent:agent /opt/steward

COPY --from=builder /steward /steward

USER agent
WORKDIR /opt/steward

EXPOSE 2112

ENTRYPOINT ["/steward"]

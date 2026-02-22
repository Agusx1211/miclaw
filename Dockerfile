FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /tmp/miclaw ./cmd/miclaw

FROM alpine:3.21
RUN adduser -D -u 1000 miclaw
COPY --from=builder /tmp/miclaw /usr/local/bin/miclaw
WORKDIR /workspace
USER miclaw
ENTRYPOINT ["miclaw"]

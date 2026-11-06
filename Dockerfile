# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24 AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o /app/server cmd/server/main.go

# Run stage
FROM alpine:3.24

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/server /app/server
COPY --from=builder /app/migrations /app/migrations

EXPOSE 8080

ENTRYPOINT ["/app/server"]

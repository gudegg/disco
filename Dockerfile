FROM docker.1ms.run/library/golang:1.21-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/config-center .

FROM alpine:3.20

RUN addgroup -S app && adduser -S -G app app && apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/config-center ./config-center
COPY --from=builder /src/static ./static

ENV GIN_MODE=release

EXPOSE 8080

USER app

CMD ["./config-center"]

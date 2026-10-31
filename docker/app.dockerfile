FROM golang:1.27.3-alpine3.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd \
    && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=builder /out/server /out/migrate /usr/local/bin/
USER app
EXPOSE 8080
CMD ["server"]

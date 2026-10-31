FROM golang:1.27.3-alpine3.24
WORKDIR /app
RUN go install github.com/air-verse/air@v1.67.4
COPY go.mod go.sum ./
RUN go mod download && go mod verify
CMD ["air", "-c", ".air.toml"]

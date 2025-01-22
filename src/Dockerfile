FROM golang:alpine as builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN go build

FROM alpine:latest
COPY --from=builder /app/mc-docker-autostart /app/mc-docker-autostart
WORKDIR /app

EXPOSE 25565

CMD ["/app/mc-docker-autostart"]

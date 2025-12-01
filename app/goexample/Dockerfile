FROM golang:1.25.0-alpine

WORKDIR /app

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go install github.com/githubnemo/CompileDaemon@latest

COPY . .

ENTRYPOINT CompileDaemon -log-prefix=false -build="go build ./cmd/app" -command="./app"

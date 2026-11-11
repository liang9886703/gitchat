FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /gitchat .
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S gitchat && adduser -S -G gitchat gitchat && mkdir /data && chown gitchat:gitchat /data
USER gitchat
COPY --from=build /gitchat /usr/local/bin/gitchat
ENV GITCHAT_ADDR=0.0.0.0:8002 DATA_DIR=/data
EXPOSE 8002
ENTRYPOINT ["gitchat"]

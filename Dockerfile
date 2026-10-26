FROM golang:1.26.10-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/secunda ./cmd/app

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=build /out/secunda /usr/local/bin/secunda
USER app
EXPOSE 8080
ENTRYPOINT ["secunda"]

FROM golang:1.26.10-bookworm
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "-race", "-tags=integration", "-count=1", "-v", "./integration-test/..."]

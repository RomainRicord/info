# Stage 1: Build
FROM golang:1.25.1 AS builder

WORKDIR /build
COPY go.mod ./
COPY main.go ./

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o info_go .

# Stage 2: Runtime
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /build/info_go .

EXPOSE 8090

ENV PORT=8090

CMD ["./info_go"]

FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-s -w" -o mihomo-proxy-pool 

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/mihomo-proxy-pool .
RUN chmod +x ./mihomo-proxy-pool
EXPOSE 9999
CMD ["./mihomo-proxy-pool"]
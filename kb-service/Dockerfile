# ── 构建阶段 ──────────────────────────────────────────────────────────────────
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o kb-service ./main.go

# ── 运行阶段 ──────────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /app/kb-service .

EXPOSE 9200

ENV PORT=9200
ENV REDIS_ADDR="redis:6379"
ENV REDIS_PASSWORD=""
ENV REDIS_DB="0"
ENV EMBED_SERVICE_URL=""
ENV PYTHON_PARSER_URL=""

CMD ["./kb-service"]

# ============================================================
# Nebula Agent — 风格学习 + 记忆引擎（Go，零外部依赖）
# 端口: 8730
# ============================================================

# ── 构建阶段 ──
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/nebula-server ./cmd/nebula-server

# ── 运行阶段 ──
FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/nebula-server ./nebula-server
COPY --from=builder /src/web ./web

EXPOSE 8730
VOLUME ["/app/data"]

# API Key 从环境变量 DEEPSEEK_API_KEY 读取（见根目录 .env.example）
ENTRYPOINT ["./nebula-server", "--data", "/app/data", "--port", "8730"]

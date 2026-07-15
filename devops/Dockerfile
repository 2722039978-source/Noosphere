# ============================================================
# DevOps Agent — 运维记忆 + 故障诊断（Go）
# 端口: 8740
#
# 注意：本服务通过 go.mod replace 依赖 ../nebula，
#       因此构建上下文必须是仓库根目录：
#       docker build -f devops/Dockerfile .
#       （docker-compose.yml 已正确配置，无需手动处理）
# ============================================================

# ── 构建阶段 ──
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY nebula/ ./nebula/
COPY devops/ ./devops/
WORKDIR /src/devops
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/devops-server ./cmd/devops-server

# ── 运行阶段 ──
FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/devops-server ./devops-server
COPY --from=builder /src/devops/web ./web

EXPOSE 8740
VOLUME ["/app/devops-memory"]

ENTRYPOINT ["./devops-server", "--port", "8740", "--memory-dir", "/app/devops-memory"]

# ============================================================
# CodeLens AI — 代码结构图谱 + RAG 问答（Python / FastAPI）
# 端口: 8765 (HTTP) / 8766 (WebSocket)
# ============================================================
FROM python:3.12-slim

WORKDIR /app

# plyvel 依赖 LevelDB 头文件；tree-sitter 语言包需要编译工具链
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
        libleveldb-dev \
        git \
    && rm -rf /var/lib/apt/lists/*

# 先装依赖，充分利用 Docker 层缓存
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8765 8766

# 容器内无浏览器，禁用自动打开
CMD ["python", "-m", "src.main", "serve", "--host", "0.0.0.0", "--port", "8765", "--no-browser"]

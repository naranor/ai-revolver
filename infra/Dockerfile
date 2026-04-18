# Stage 1: Build Frontend
FROM node:18-alpine as frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json ./
# Если есть package-lock.json, копируем его
COPY frontend/package-lock.json* ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Backend
FROM golang:1.24-alpine as backend-builder
WORKDIR /app
RUN apk add --no-cache build-base git bash
COPY backend/ ./backend/
COPY build.sh .
RUN chmod +x build.sh && ./build.sh --no-test

# Stage 3: Final Image
FROM alpine:latest
WORKDIR /app

# Установка зависимостей
RUN apk add --no-cache nginx supervisor

# Копирование бинарника
COPY --from=backend-builder /app/build/ai-revolver ./ai-proxy
# Копирование конфигов
COPY infra/supervisord.conf .
COPY infra/nginx.conf /etc/nginx/http.d/default.conf
# Копирование собранного frontend
COPY --from=frontend-builder /app/frontend/build ./frontend

# Создание директории для данных и копирование дефолтного конфига
RUN mkdir -p data
COPY backend/config/config.json.example ./data/config.json

VOLUME /app/data
EXPOSE 8080
CMD ["/usr/bin/supervisord", "-c", "/app/supervisord.conf"]

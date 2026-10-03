# 构建上下文为 frontend/（compose: build: ./frontend）
# ---------- 构建阶段 ----------
FROM node:24-alpine AS build
WORKDIR /app

# 站点路径前缀。子域名独立部署（jizhang.zhanghongyun.cn）保持根路径 "/"；
# 若要挂到子路径（如 /jizhang/），构建时传 --build-arg VITE_BASE=/jizhang/，
# 同时改 nginx.conf 的路径与 api 环境变量 COOKIE_PATH。
ARG VITE_BASE=/
ENV VITE_BASE=$VITE_BASE

# 先拷依赖清单，利用层缓存
COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# ---------- 运行阶段：nginx 托管 dist + 反代 /api/v1/ ----------
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80

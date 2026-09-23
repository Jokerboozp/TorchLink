# 执行当前脚本步骤。
FROM node:22-alpine AS build
# 执行当前脚本步骤。
WORKDIR /app
# 执行当前脚本步骤。
COPY package.json package-lock.json ./
# 执行当前脚本步骤。
RUN npm ci
# 执行当前脚本步骤。
COPY . .
# 执行当前脚本步骤。
RUN npm run build

# 执行当前脚本步骤。
FROM nginx:1.27-alpine
# 执行当前脚本步骤。
COPY nginx.conf /etc/nginx/templates/default.conf.template
# 执行当前脚本步骤。
COPY --from=build /app/dist /usr/share/nginx/html
# 执行当前脚本步骤。
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:8080/health/live >/dev/null || exit 1
# 执行当前脚本步骤。
CMD ["nginx", "-g", "daemon off;"]

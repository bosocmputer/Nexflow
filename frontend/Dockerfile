# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# VITE_PHASE is baked into the JS bundle at build time by Vite.
# Pass it via docker-compose build args (see docker-compose.yml args section).
ARG VITE_PHASE=99
ENV VITE_PHASE=${VITE_PHASE}
ARG VITE_ENABLE_SALES_ORDERS=true
ENV VITE_ENABLE_SALES_ORDERS=${VITE_ENABLE_SALES_ORDERS}
ARG VITE_ENABLE_SHOPEE_EXCEL=true
ENV VITE_ENABLE_SHOPEE_EXCEL=${VITE_ENABLE_SHOPEE_EXCEL}
ARG VITE_ENABLE_SHOPEE_REALTIME_OPS=false
ENV VITE_ENABLE_SHOPEE_REALTIME_OPS=${VITE_ENABLE_SHOPEE_REALTIME_OPS}
ARG VITE_ENABLE_LAZADA_EXCEL=true
ENV VITE_ENABLE_LAZADA_EXCEL=${VITE_ENABLE_LAZADA_EXCEL}
ARG VITE_ENABLE_TIKTOK_EXCEL=true
ENV VITE_ENABLE_TIKTOK_EXCEL=${VITE_ENABLE_TIKTOK_EXCEL}
ARG VITE_ENABLE_CHAT=true
ENV VITE_ENABLE_CHAT=${VITE_ENABLE_CHAT}
ARG VITE_ENABLE_REMARK2=false
ENV VITE_ENABLE_REMARK2=${VITE_ENABLE_REMARK2}

COPY package.json package-lock.json ./
RUN npm ci --legacy-peer-deps

COPY . .

RUN npm run build

# Runtime stage — nginx
FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]

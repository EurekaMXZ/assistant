FROM nginx:1.28-alpine

RUN mkdir -p /etc/nginx/snippets-enabled

COPY api.conf /etc/nginx/nginx.conf
COPY amap-service.conf.example /etc/nginx/templates/amap-service.conf.template

ENV NGINX_ENVSUBST_FILTER=AMAP_SECURITY_JS_CODE \
    NGINX_ENVSUBST_OUTPUT_DIR=/etc/nginx/snippets-enabled

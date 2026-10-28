FROM nginx:1.27-alpine
COPY deploy/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY frontend/dist/ /usr/share/nginx/html/
EXPOSE 8080

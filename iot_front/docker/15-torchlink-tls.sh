#!/bin/sh
# Enables HTTPS on 8443 when a certificate is mounted at /etc/torchlink/tls.
# Plain HTTP on 8080 stays for the health check; with IOT_WEB_TLS_REDIRECT
# (default true) other HTTP requests are redirected to HTTPS.
set -eu
dir=/etc/nginx/torchlink
mkdir -p "$dir"
cert=/etc/torchlink/tls/tls.crt
key=/etc/torchlink/tls/tls.key
printf 'listen 8080;\n' > "$dir/listen.conf"
: > "$dir/redirect.conf"
printf 'map "$scheme:$uri" $torchlink_redirect {\n    default 0;\n}\nmap $https $torchlink_hsts {\n    default "";\n}\n' > "$dir/maps.conf"
if [ -s "$cert" ] && [ -s "$key" ]; then
  cat >> "$dir/listen.conf" <<CONF
listen 8443 ssl;
ssl_certificate $cert;
ssl_certificate_key $key;
ssl_protocols TLSv1.2 TLSv1.3;
ssl_session_cache shared:torchlink_tls:10m;
CONF
  if [ "${IOT_WEB_TLS_REDIRECT:-true}" = "true" ]; then
    port="${IOT_WEB_HTTPS_PORT:-8443}"
    target='https://$host'
    [ "$port" = 443 ] || target="https://\$host:$port"
    printf 'map "$scheme:$uri" $torchlink_redirect {\n    default 0;\n    "~^http:/(?!health/)" 1;\n}\nmap $https $torchlink_hsts {\n    on "max-age=31536000";\n    default "";\n}\n' > "$dir/maps.conf"
    printf 'if ($torchlink_redirect) {\n    return 308 %s$request_uri;\n}\n' "$target" > "$dir/redirect.conf"
  fi
  echo "torchlink: HTTPS enabled on 8443"
else
  echo "torchlink: no certificate at $cert; serving HTTP only"
fi

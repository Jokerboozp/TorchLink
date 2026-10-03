#!/usr/bin/env bash
# Creates a self-signed certificate for the Web (HTTPS) and MQTT (MQTTS/WSS)
# listeners. Production deployments should prefer a certificate issued by a
# trusted CA: place it as tls.crt (full chain) and tls.key in the same folder.
set -Eeuo pipefail
dir=tls
days=825
names=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) dir="${2:?--dir 需要目录}"; shift 2 ;;
    --days) days="${2:?--days 需要天数}"; shift 2 ;;
    --host) names+=("${2:?--host 需要域名或 IP}"); shift 2 ;;
    -h|--help) echo "用法：generate-tls-cert.sh --host <域名或IP> [--host ...] [--dir tls] [--days 825]"; exit 0 ;;
    *) echo "未知参数：$1" >&2; exit 1 ;;
  esac
done
(( ${#names[@]} )) || { echo '请至少用 --host 指定一个设备和浏览器访问平台使用的域名或 IP。' >&2; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo '需要 openssl。' >&2; exit 1; }
if [[ -e "$dir/tls.crt" || -e "$dir/tls.key" ]]; then
  echo "$dir 中已有证书，未覆盖；如需重新生成请先移走旧文件。" >&2
  exit 1
fi
mkdir -p "$dir"
san=""
for name in "${names[@]}"; do
  if [[ "$name" =~ ^[0-9.]+$ || "$name" == *:* ]]; then san+="IP:$name,"; else san+="DNS:$name,"; fi
done
san="${san%,}"
umask 077
openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days "$days" \
  -keyout "$dir/tls.key" -out "$dir/tls.crt" -subj "/CN=${names[0]}" \
  -addext "subjectAltName=$san" -addext "extendedKeyUsage=serverAuth" >/dev/null 2>&1
chmod 0644 "$dir/tls.crt"
# The broker and web containers read the key as non-root users.
chmod 0644 "$dir/tls.key"
echo "已生成自签名证书：$dir/tls.crt（$san），有效期 $days 天。设备和浏览器需信任该证书。"

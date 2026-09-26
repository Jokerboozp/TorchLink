#!/usr/bin/env bash
# Public bundles carry a template; credentials are created on the target only.
set -Eeuo pipefail
umask 077
bundle_dir="${1:?请指定离线包目录}"
env_file="$bundle_dir/.env.offline"
if [[ -f "$env_file" ]]; then
  echo '保留已有 .env.offline 配置。'
  exit 0
fi
[[ ! -e "$env_file" && ! -L "$env_file" ]] || { echo '配置路径已存在且不是普通文件。' >&2; exit 1; }
template="$bundle_dir/.env.offline.template"
[[ -f "$template" ]] || { echo '缺少公开离线包配置模板。' >&2; exit 1; }
temporary="$(mktemp "$bundle_dir/.env.offline.tmp.XXXXXX")"
trap 'rm -f -- "$temporary"' EXIT
while IFS= read -r line || [[ -n "$line" ]]; do
  if [[ "$line" == *'__TORCHLINK_RANDOM_HEX__'* ]]; then
    secret="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
    line="${line//__TORCHLINK_RANDOM_HEX__/$secret}"
  fi
  printf '%s\n' "$line"
done < "$template" > "$temporary"
# Atomic publication without overwriting an existing or concurrently created file.
ln "$temporary" "$env_file"
echo '已在本机生成独立凭据；管理员用户名和密码见 .env.offline。'

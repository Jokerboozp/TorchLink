#!/usr/bin/env bash

env_comments_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
deployment_env_comments_file="$env_comments_dir/env-comments.tsv"
unset env_comments_dir

deployment_env_comment() {
  local key="$1" description
  description="$(awk -F '\t' -v key="$key" '$1 == key { print substr($0, index($0, "\t") + 1); exit }' "$deployment_env_comments_file")"
  if [ -n "$description" ]; then
    printf '%s' "$description"
  else
    printf '自定义配置项 %s' "$key"
  fi
}

annotate_deployment_env_file() {
  local env_path="$1" tmp line key
  tmp="$(mktemp "${env_path}.comments.XXXXXX")"
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%$'\r'}"
    case "$line" in '# 配置说明：'*) continue;; esac
    if [[ "$line" =~ ^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*= ]]; then
      key="${BASH_REMATCH[2]}"
      printf '# 配置说明：%s\n' "$(deployment_env_comment "$key")" >> "$tmp"
    fi
    printf '%s\n' "$line" >> "$tmp"
  done < "$env_path"
  mv -- "$tmp" "$env_path"
}

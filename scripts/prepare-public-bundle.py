#!/usr/bin/env python3
"""Prepare a freshly generated CI bundle for public distribution, in place."""
import json
from pathlib import Path
import shutil
import sys


ADMIN_PASSWORD_KEYS = frozenset((
    'IOT_ADMIN_PASSWORD', 'SERVICE_ADMIN_PASSWORD', 'POSTGRES_PASSWORD',
    'REDIS_PASSWORD', 'CLICKHOUSE_PASSWORD', 'MINIO_ROOT_PASSWORD',
    'MINIO_DR_ROOT_PASSWORD', 'EMQX_DASHBOARD_PASSWORD', 'GRAFANA_ADMIN_PASSWORD',
    'IOT_MQTT_TOOL_PASSWORD', 'IOT_KAFKA_SASL_PASSWORD', 'IOT_KAFKA_ADMIN_PASSWORD',
))
ADMIN_USERNAME_KEYS = frozenset((
    'SERVICE_ADMIN_USER', 'MINIO_ROOT_USER', 'MINIO_DR_ROOT_USER',
    'EMQX_DASHBOARD_USER', 'GRAFANA_ADMIN_USER', 'IOT_ADMIN_USER',
    'IOT_MQTT_TOOL_USERNAME', 'IOT_KAFKA_SASL_USERNAME', 'IOT_KAFKA_ADMIN_USERNAME',
))


def prepare(bundle: Path) -> None:
    manifest_path = bundle / 'manifest.json'
    manifest = json.loads(manifest_path.read_text())
    if manifest.get('generatedCredentials') is not True:
        raise ValueError('只允许发布自动生成配置的全新包，不能发布现场配置')
    env_path = bundle / '.env.offline'
    public_lines = []
    for line in env_path.read_text().splitlines():
        if not line or line.startswith('#'):
            public_lines.append(line)
            continue
        key, value = line.split('=', 1)
        if key in ('DEEPSEEK_API_KEY', 'IOT_AI_API_KEY', 'IOT_EMBEDDING_API_KEY'):
            if value.strip("'\""):
                raise ValueError('发布包不能包含 API Key')
        elif key in ADMIN_PASSWORD_KEYS:
            # Publish only the agreed installation default, never a password
            # copied from the packaging machine, even in generated configs.
            value = 'admin123'
        elif key in ADMIN_USERNAME_KEYS:
            value = 'admin'
        elif key == 'IOT_VIDEO_CREDENTIAL_KEY':
            # 32 random bytes (base64) that seal camera and GB28181 device passwords.
            value = '__TORCHLINK_RANDOM_BASE64_32__'
        elif key in ('IOT_EMQX_API_KEY',) or key.endswith(('_PASSWORD', '_TOKEN', '_SECRET', '_SECRETS')):
            value = '__TORCHLINK_RANDOM_HEX__'
        public_lines.append(f'{key}={value}')
    (bundle / '.env.offline.template').write_text('\n'.join(public_lines) + '\n')
    scripts = Path(__file__).resolve().parent
    for name in ('init-offline-env.sh', 'init-offline-env.ps1'):
        shutil.copyfile(scripts / name, bundle / 'scripts' / name)
    (bundle / 'OFFLINE-CREDENTIALS.txt').write_text(
        '公开包只包含新安装的默认账号配置，不包含打包机器的实际部署凭据。\n'
        '管理员和工具账号默认 admin/admin123；应用数据库账号仍为 iot，连接密码默认 admin123。\n'
        '首次部署自动生成 .env.offline，JWT、内部令牌和加密密钥在目标机器独立随机生成。\n'
        '升级已有部署时，请先将原 .env.offline 放入本目录，保留原项目和数据卷。\n'
        'DeepSeek API Key 请在部署后通过模型管理填写。\n'
    )
    env_path.unlink()
    manifest['generatedCredentials'] = False
    manifest['credentialsGeneratedOnTarget'] = True
    manifest['envTemplate'] = '.env.offline.template'
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    prepare(Path(sys.argv[1]))

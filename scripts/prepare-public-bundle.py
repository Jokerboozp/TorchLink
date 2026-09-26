#!/usr/bin/env python3
"""Prepare a freshly generated CI bundle for public distribution, in place."""
import json
from pathlib import Path
import shutil
import sys


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
        if key in ('DEEPSEEK_API_KEY', 'IOT_AI_API_KEY'):
            if value.strip("'\""):
                raise ValueError('发布包不能包含 API Key')
        elif key == 'IOT_VIDEO_PLATFORM_SECRETS':
            value = 'video-platform-1:__TORCHLINK_RANDOM_HEX__'
        elif key.endswith(('_PASSWORD', '_TOKEN', '_SECRET', '_SECRETS')):
            value = '__TORCHLINK_RANDOM_HEX__'
        public_lines.append(f'{key}={value}')
    (bundle / '.env.offline.template').write_text('\n'.join(public_lines) + '\n')
    scripts = Path(__file__).resolve().parent
    for name in ('init-offline-env.sh', 'init-offline-env.ps1'):
        shutil.copyfile(scripts / name, bundle / 'scripts' / name)
    (bundle / 'OFFLINE-CREDENTIALS.txt').write_text(
        '公开包不包含部署凭据。首次部署自动生成 .env.offline，管理员密码也随机生成。\n'
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

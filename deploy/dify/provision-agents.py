"""Run inside the Dify 1.17.1 API container using its configured Python runtime.

Uses Dify services for import, publishing, tool registration and Skill binding.
Input DSL may contain secrets: stage it privately and remove it after use.
"""
import argparse
import json
from pathlib import Path

from app import app
from flask import g
from sqlalchemy import select
from extensions.ext_database import db
from models import Account, Tenant
from models.model import App
from models.agent import Agent, AgentConfigDraft
from models.tools import WorkflowToolProvider
from models.skill import Skill
from models.agent_config_entities import AgentSoulConfig
from services.app_dsl_service import AppDslService
from services.app_service import AppService
from services.workflow_service import WorkflowService
from services.tools.workflow_tools_manage_service import WorkflowToolManageService
from core.tools.entities.tool_entities import WorkflowToolParameterConfiguration
from services.agent.composer_service import AgentComposerService
from services.entities.agent_entities import ComposerSavePayload
from services.skill_management_service import (
    SkillManagementService, SkillCreatePayload, SkillDraftTreePayload, SkillPublishPayload,
)

parser = argparse.ArgumentParser()
parser.add_argument('--assets', required=True)
parser.add_argument('--anchor-app', required=True)
parser.add_argument('--direct-agent', required=True)
parser.add_argument('--skill-agent', required=True)
args = parser.parse_args()
assets = Path(args.assets)
manifest = json.loads((assets / 'manifest.json').read_text(encoding='utf-8'))

with app.test_request_context():
    anchor = db.session.get(App, args.anchor_app)
    assert anchor and anchor.name == '炬联 IoT · 运维助手'
    account = db.session.get(Account, anchor.created_by)
    tenant = db.session.get(Tenant, anchor.tenant_id)
    account.set_current_tenant_with_session(tenant, session=db.session)
    g._login_user = account
    tenant_id, user_id = anchor.tenant_id, account.id
    agents = [db.session.get(Agent, i) for i in [args.direct_agent, args.skill_agent]]
    assert all(a and a.tenant_id == tenant_id and a.created_by == user_id and a.name.startswith('炬联 IoT · ') for a in agents)
    draft = db.session.scalar(select(AgentConfigDraft).where(AgentConfigDraft.agent_id == args.direct_agent))
    base_model = draft.config_snapshot_dict.get('model') if draft else None
    assert base_model, 'Select a model on the direct Agent before provisioning'
    base_model['model_settings'] = {**base_model.get('model_settings', {}), 'thinking': False, 'temperature': 0.2, 'max_tokens': 8192}
    tools, skill_ids, result = [], [], {'tools': [], 'skills': [], 'agents': []}

    for item in manifest['records']:
        tool = db.session.scalar(select(WorkflowToolProvider).where(WorkflowToolProvider.tenant_id == tenant_id, WorkflowToolProvider.name == item['toolName']))
        if tool is None:
            dsl = (assets / (item['toolName'] + '.yml')).read_text(encoding='utf-8')
            data = json.loads(dsl)
            existing = db.session.scalar(select(App).where(App.tenant_id == tenant_id, App.name == data['app']['name']))
            if existing is None:
                imported = AppDslService(db.session).import_app(account=account, import_mode='yaml-content', yaml_content=dsl)
                assert imported.app_id, f"Import failed: {imported.status}"
                db.session.commit()
                existing = db.session.get(App, imported.app_id)
            assert existing.created_by == user_id and existing.name.startswith('炬联 IoT 工具 · ')
            AppService().update_app_site_status(existing, False, session=db.session)
            AppService().update_app_api_status(existing, False, session=db.session)
            if existing.workflow_id is None:
                published = WorkflowService().publish_workflow(session=db.session, app_model=existing, account=account, marked_name='Agent 数据工具')
                db.session.flush()
                existing.workflow_id = published.id
                db.session.commit()
            WorkflowToolManageService.create_workflow_tool(
                user_id=user_id, tenant_id=tenant_id, workflow_app_id=existing.id,
                name=item['toolName'], label='IoT ' + item['title'], icon={'content': '🔧', 'background': '#FFEAD5'},
                description=item['toolDescription'],
                parameters=[WorkflowToolParameterConfiguration(name='question', description='本轮用户完整任务原话。保存规则草稿时不得改写。', form='llm'),
                            WorkflowToolParameterConfiguration(name='plan', description='JSON 字符串；首次取上下文用 {"calls":[]}；补充用 {"calls":[{"name":"可用工具","arguments":{}}]}。', form='llm')],
            )
            tool = db.session.scalar(select(WorkflowToolProvider).where(WorkflowToolProvider.tenant_id == tenant_id, WorkflowToolProvider.name == item['toolName']))
        assert tool and tool.user_id == user_id
        tools.append({'enabled': True, 'provider_type': 'workflow', 'provider_id': tool.id, 'tool_name': item['toolName'], 'description': item['toolDescription']})
        result['tools'].append({'name': item['toolName'], 'provider_id': tool.id, 'app_id': tool.app_id})

        skills = SkillManagementService(session=db.session)
        skill = db.session.scalar(select(Skill).where(Skill.tenant_id == tenant_id, Skill.name == item['skillName']))
        if skill is None:
            created = skills.create_skill(tenant_id=tenant_id, user_id=user_id, payload=SkillCreatePayload(name=item['skillName'], display_name='炬联 IoT · ' + item['title'] + ' Skill', description=item['description']))
            skill_id = created['id']
        else:
            assert skill.created_by == user_id and skill.display_name.startswith('炬联 IoT · ')
            skill_id = skill.id
        skills.replace_draft_tree(tenant_id=tenant_id, user_id=user_id, skill_id=skill_id, payload=SkillDraftTreePayload(files=[{'path': 'SKILL.md', 'kind': 'file', 'storage': 'text', 'mime_type': 'text/markdown', 'content': item['content']}]))
        skills.publish_skill(tenant_id=tenant_id, user_id=user_id, skill_id=skill_id, payload=SkillPublishPayload(version_name='IoT 对比演示', publish_note='按实际平台工具与知识边界执行'))
        skill_ids.append(skill_id)
        result['skills'].append({'name': item['skillName'], 'id': skill_id})
        print('READY', item['toolName'], skill_id, flush=True)

    for index, agent in enumerate(agents):
        soul = AgentSoulConfig.model_validate({'prompt': {'system_prompt': manifest['directPrompt' if index == 0 else 'skillPrompt']}, 'model': base_model, 'tools': {'dify_tools': tools, 'cli_tools': []}})
        AgentComposerService.save_agent_composer(session=db.session, tenant_id=tenant_id, agent_id=agent.id, account_id=user_id,
            payload=ComposerSavePayload(variant='agent_app', save_strategy='save_to_current_version', agent_soul=soul))
        db.session.commit()
        SkillManagementService(session=db.session).replace_agent_bindings(tenant_id=tenant_id, user_id=user_id, agent_id=agent.id, skill_ids=skill_ids if index else [])
        backing = db.session.get(App, agent.backing_app_id or agent.app_id)
        assert backing and backing.tenant_id == tenant_id
        AppService().update_app_site_status(backing, False, session=db.session)
        AppService().update_app_api_status(backing, False, session=db.session)
        AgentComposerService.publish_agent_app_draft(session=db.session, tenant_id=tenant_id, agent_id=agent.id, account_id=user_id, version_note='IoT 同工具同模型对比')
        db.session.commit()
        # First native Agent publish enables access points; restore workspace-only use.
        db.session.refresh(backing)
        AppService().update_app_site_status(backing, False, session=db.session)
        AppService().update_app_api_status(backing, False, session=db.session)
        result['agents'].append({'id': agent.id, 'app_id': backing.id, 'skills': len(skill_ids) if index else 0})
        print('PUBLISHED', agent.id, flush=True)
    (assets / 'deployment-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False))

"""Run inside the Dify 1.17.1 API container using its configured Python runtime.

Uses Dify services for import, publishing, tool registration and Skill binding.
Input DSL may contain secrets: stage it privately and remove it after use.
"""
# 引入当前代码需要的依赖。
import argparse
# 引入当前代码需要的依赖。
import json
# 引入当前代码需要的依赖。
from pathlib import Path

# 引入当前代码需要的依赖。
from app import app
# 引入当前代码需要的依赖。
from flask import g
# 引入当前代码需要的依赖。
from sqlalchemy import select
# 引入当前代码需要的依赖。
from extensions.ext_database import db
# 引入当前代码需要的依赖。
from models import Account, Tenant
# 引入当前代码需要的依赖。
from models.model import App
# 引入当前代码需要的依赖。
from models.agent import Agent, AgentConfigDraft
# 引入当前代码需要的依赖。
from models.tools import WorkflowToolProvider
# 引入当前代码需要的依赖。
from models.skill import Skill
# 引入当前代码需要的依赖。
from models.agent_config_entities import AgentSoulConfig
# 引入当前代码需要的依赖。
from services.app_dsl_service import AppDslService
# 引入当前代码需要的依赖。
from services.app_service import AppService
# 引入当前代码需要的依赖。
from services.workflow_service import WorkflowService
# 引入当前代码需要的依赖。
from services.tools.workflow_tools_manage_service import WorkflowToolManageService
# 引入当前代码需要的依赖。
from core.tools.entities.tool_entities import WorkflowToolParameterConfiguration
# 引入当前代码需要的依赖。
from services.agent.composer_service import AgentComposerService
# 引入当前代码需要的依赖。
from services.entities.agent_entities import ComposerSavePayload
# 引入当前代码需要的依赖。
from services.skill_management_service import (
    # 执行当前语句并推进处理流程。
    SkillManagementService, SkillCreatePayload, SkillDraftTreePayload, SkillPublishPayload,
# 结束当前表达式或代码块。
)

# 更新 parser 的值。
parser = argparse.ArgumentParser()
# 执行当前语句并推进处理流程。
parser.add_argument('--assets', required=True)
# 执行当前语句并推进处理流程。
parser.add_argument('--anchor-app', required=True)
# 执行当前语句并推进处理流程。
parser.add_argument('--direct-agent', required=True)
# 执行当前语句并推进处理流程。
parser.add_argument('--skill-agent', required=True)
# 更新 args 的值。
args = parser.parse_args()
# 更新 assets 的值。
assets = Path(args.assets)
# 更新 manifest 的值。
manifest = json.loads((assets / 'manifest.json').read_text(encoding='utf-8'))

# 执行当前语句并推进处理流程。
with app.test_request_context():
    # 更新 anchor 的值。
    anchor = db.session.get(App, args.anchor_app)
    # 执行当前语句并推进处理流程。
    assert anchor and anchor.name == '炬联 IoT · 运维助手'
    # 更新 account 的值。
    account = db.session.get(Account, anchor.created_by)
    # 更新 tenant 的值。
    tenant = db.session.get(Tenant, anchor.tenant_id)
    # 执行当前语句并推进处理流程。
    account.set_current_tenant_with_session(tenant, session=db.session)
    # 更新 g._login_user 的值。
    g._login_user = account
    # 更新 user_id 的值。
    tenant_id, user_id = anchor.tenant_id, account.id
    # 更新 agents 的值。
    agents = [db.session.get(Agent, i) for i in [args.direct_agent, args.skill_agent]]
    # 执行当前语句并推进处理流程。
    assert all(a and a.tenant_id == tenant_id and a.created_by == user_id and a.name.startswith('炬联 IoT · ') for a in agents)
    # 更新 draft 的值。
    draft = db.session.scalar(select(AgentConfigDraft).where(AgentConfigDraft.agent_id == args.direct_agent))
    # 更新 base_model 的值。
    base_model = draft.config_snapshot_dict.get('model') if draft else None
    # 执行当前语句并推进处理流程。
    assert base_model, 'Select a model on the direct Agent before provisioning'
    # 执行当前语句并推进处理流程。
    base_model['model_settings'] = {**base_model.get('model_settings', {}), 'thinking': False, 'temperature': 0.2, 'max_tokens': 8192}
    # 更新 result 的值。
    tools, skill_ids, result = [], [], {'tools': [], 'skills': [], 'agents': []}

    # 循环处理当前数据。
    for item in manifest['records']:
        # 更新 tool 的值。
        tool = db.session.scalar(select(WorkflowToolProvider).where(WorkflowToolProvider.tenant_id == tenant_id, WorkflowToolProvider.name == item['toolName']))
        # 判断条件并选择处理分支。
        if tool is None:
            # 更新 dsl 的值。
            dsl = (assets / (item['toolName'] + '.yml')).read_text(encoding='utf-8')
            # 更新 data 的值。
            data = json.loads(dsl)
            # 更新 existing 的值。
            existing = db.session.scalar(select(App).where(App.tenant_id == tenant_id, App.name == data['app']['name']))
            # 判断条件并选择处理分支。
            if existing is None:
                # 更新 imported 的值。
                imported = AppDslService(db.session).import_app(account=account, import_mode='yaml-content', yaml_content=dsl)
                # 执行当前语句并推进处理流程。
                assert imported.app_id, f"Import failed: {imported.status}"
                # 执行当前语句并推进处理流程。
                db.session.commit()
                # 更新 existing 的值。
                existing = db.session.get(App, imported.app_id)
            # 执行当前语句并推进处理流程。
            assert existing.created_by == user_id and existing.name.startswith('炬联 IoT 工具 · ')
            # 执行当前语句并推进处理流程。
            AppService().update_app_site_status(existing, False, session=db.session)
            # 执行当前语句并推进处理流程。
            AppService().update_app_api_status(existing, False, session=db.session)
            # 判断条件并选择处理分支。
            if existing.workflow_id is None:
                # 更新 published 的值。
                published = WorkflowService().publish_workflow(session=db.session, app_model=existing, account=account, marked_name='Agent 数据工具')
                # 执行当前语句并推进处理流程。
                db.session.flush()
                # 更新 existing.workflow_id 的值。
                existing.workflow_id = published.id
                # 执行当前语句并推进处理流程。
                db.session.commit()
            # 执行当前语句并推进处理流程。
            WorkflowToolManageService.create_workflow_tool(
                # 更新 user_id 的值。
                user_id=user_id, tenant_id=tenant_id, workflow_app_id=existing.id,
                # 更新 name 的值。
                name=item['toolName'], label='IoT ' + item['title'], icon={'content': '🔧', 'background': '#FFEAD5'},
                # 更新 description 的值。
                description=item['toolDescription'],
                # 更新 parameters 的值。
                parameters=[WorkflowToolParameterConfiguration(name='question', description='本轮用户完整任务原话。保存规则草稿时不得改写。', form='llm'),
                            # 执行当前语句并推进处理流程。
                            WorkflowToolParameterConfiguration(name='plan', description='JSON 字符串；首次取上下文用 {"calls":[]}；补充用 {"calls":[{"name":"可用工具","arguments":{}}]}。', form='llm')],
            # 结束当前表达式或代码块。
            )
            # 更新 tool 的值。
            tool = db.session.scalar(select(WorkflowToolProvider).where(WorkflowToolProvider.tenant_id == tenant_id, WorkflowToolProvider.name == item['toolName']))
        # 执行当前语句并推进处理流程。
        assert tool and tool.user_id == user_id
        # 执行当前语句并推进处理流程。
        tools.append({'enabled': True, 'provider_type': 'workflow', 'provider_id': tool.id, 'tool_name': item['toolName'], 'description': item['toolDescription']})
        # 执行当前语句并推进处理流程。
        result['tools'].append({'name': item['toolName'], 'provider_id': tool.id, 'app_id': tool.app_id})

        # 更新 skills 的值。
        skills = SkillManagementService(session=db.session)
        # 更新 skill 的值。
        skill = db.session.scalar(select(Skill).where(Skill.tenant_id == tenant_id, Skill.name == item['skillName']))
        # 判断条件并选择处理分支。
        if skill is None:
            # 更新 created 的值。
            created = skills.create_skill(tenant_id=tenant_id, user_id=user_id, payload=SkillCreatePayload(name=item['skillName'], display_name='炬联 IoT · ' + item['title'] + ' Skill', description=item['description']))
            # 更新 skill_id 的值。
            skill_id = created['id']
        # 执行当前语句并推进处理流程。
        else:
            # 执行当前语句并推进处理流程。
            assert skill.created_by == user_id and skill.display_name.startswith('炬联 IoT · ')
            # 更新 skill_id 的值。
            skill_id = skill.id
        # 执行当前语句并推进处理流程。
        skills.replace_draft_tree(tenant_id=tenant_id, user_id=user_id, skill_id=skill_id, payload=SkillDraftTreePayload(files=[{'path': 'SKILL.md', 'kind': 'file', 'storage': 'text', 'mime_type': 'text/markdown', 'content': item['content']}]))
        # 执行当前语句并推进处理流程。
        skills.publish_skill(tenant_id=tenant_id, user_id=user_id, skill_id=skill_id, payload=SkillPublishPayload(version_name='IoT 对比演示', publish_note='按实际平台工具与知识边界执行'))
        # 执行当前语句并推进处理流程。
        skill_ids.append(skill_id)
        # 执行当前语句并推进处理流程。
        result['skills'].append({'name': item['skillName'], 'id': skill_id})
        # 执行当前语句并推进处理流程。
        print('READY', item['toolName'], skill_id, flush=True)

    # 循环处理当前数据。
    for index, agent in enumerate(agents):
        # 更新 soul 的值。
        soul = AgentSoulConfig.model_validate({'prompt': {'system_prompt': manifest['directPrompt' if index == 0 else 'skillPrompt']}, 'model': base_model, 'tools': {'dify_tools': tools, 'cli_tools': []}})
        # 执行当前语句并推进处理流程。
        AgentComposerService.save_agent_composer(session=db.session, tenant_id=tenant_id, agent_id=agent.id, account_id=user_id,
            # 更新 payload 的值。
            payload=ComposerSavePayload(variant='agent_app', save_strategy='save_to_current_version', agent_soul=soul))
        # 执行当前语句并推进处理流程。
        db.session.commit()
        # 执行当前语句并推进处理流程。
        SkillManagementService(session=db.session).replace_agent_bindings(tenant_id=tenant_id, user_id=user_id, agent_id=agent.id, skill_ids=skill_ids if index else [])
        # 更新 backing 的值。
        backing = db.session.get(App, agent.backing_app_id or agent.app_id)
        # 执行当前语句并推进处理流程。
        assert backing and backing.tenant_id == tenant_id
        # 执行当前语句并推进处理流程。
        AppService().update_app_site_status(backing, False, session=db.session)
        # 执行当前语句并推进处理流程。
        AppService().update_app_api_status(backing, False, session=db.session)
        # 执行当前语句并推进处理流程。
        AgentComposerService.publish_agent_app_draft(session=db.session, tenant_id=tenant_id, agent_id=agent.id, account_id=user_id, version_note='IoT 同工具同模型对比')
        # 执行当前语句并推进处理流程。
        db.session.commit()
        # First native Agent publish enables access points; restore workspace-only use.
        # 执行当前语句并推进处理流程。
        db.session.refresh(backing)
        # 执行当前语句并推进处理流程。
        AppService().update_app_site_status(backing, False, session=db.session)
        # 执行当前语句并推进处理流程。
        AppService().update_app_api_status(backing, False, session=db.session)
        # 执行当前语句并推进处理流程。
        result['agents'].append({'id': agent.id, 'app_id': backing.id, 'skills': len(skill_ids) if index else 0})
        # 执行当前语句并推进处理流程。
        print('PUBLISHED', agent.id, flush=True)
    # 执行当前语句并推进处理流程。
    (assets / 'deployment-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    # 执行当前语句并推进处理流程。
    print(json.dumps(result, ensure_ascii=False))

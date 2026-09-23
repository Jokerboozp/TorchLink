# 执行当前语句并推进处理流程。
"""Publish workspace-only, single native-Agent-node comparison entry points."""
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
from models.workflow import Workflow
# 引入当前代码需要的依赖。
from models.agent import Agent
# 引入当前代码需要的依赖。
from services.app_dsl_service import AppDslService
# 引入当前代码需要的依赖。
from services.app_service import AppService
# 引入当前代码需要的依赖。
from services.workflow_service import WorkflowService

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

# 执行当前语句并推进处理流程。
def node(id, kind, title, x, **data):
    # 返回当前处理结果。
    return {'id': id, 'type': 'custom', 'position': {'x': x, 'y': 200},
            # 执行当前语句并推进处理流程。
            'data': {'type': kind, 'title': title, **data}}

# 执行当前语句并推进处理流程。
def graph(nodes):
    # 返回当前处理结果。
    return {'nodes': nodes, 'edges': [
        # 执行当前语句并推进处理流程。
        {'id': f'{a["id"]}-{b["id"]}', 'source': a['id'], 'target': b['id'],
         # 执行当前语句并推进处理流程。
         'sourceHandle': 'source', 'targetHandle': 'target', 'type': 'custom',
         # 执行当前语句并推进处理流程。
         'data': {'sourceType': a['data']['type'], 'targetType': b['data']['type']}}
        # 循环处理当前数据。
        for a, b in zip(nodes, nodes[1:])], 'viewport': {'x': 0, 'y': 0, 'zoom': 0.8}}

# 执行当前语句并推进处理流程。
with app.test_request_context():
    # 更新 anchor 的值。
    anchor = db.session.get(App, args.anchor_app)
    # 执行当前语句并推进处理流程。
    assert anchor and anchor.name == '炬联 IoT · 运维助手'
    # 更新 account 的值。
    account = db.session.get(Account, anchor.created_by)
    # 执行当前语句并推进处理流程。
    account.set_current_tenant_with_session(db.session.get(Tenant, anchor.tenant_id), session=db.session)
    # 更新 g._login_user 的值。
    g._login_user = account
    # 更新 results 的值。
    results = []
    # 循环处理当前数据。
    for aid, label in [(args.direct_agent, '原生 Agent'), (args.skill_agent, 'Skills Agent')]:
        # 更新 agent 的值。
        agent = db.session.get(Agent, aid)
        # 执行当前语句并推进处理流程。
        assert agent and agent.tenant_id == anchor.tenant_id and agent.created_by == account.id
        # 更新 name 的值。
        name = '炬联 IoT 体验 · ' + label
        # 更新 start 的值。
        start = node('4100000000001', 'start', '对比任务', 80, variables=[
            # 执行当前语句并推进处理流程。
            {'variable': 'question', 'label': '对比任务', 'type': 'paragraph', 'required': True, 'max_length': 32000, 'options': []}])
        # 更新 end 的值。
        end = node('4100000000003', 'end', '分析结果', 760, outputs=[
            # 执行当前语句并推进处理流程。
            {'variable': 'text', 'value_selector': [start['id'], 'question'], 'value_type': 'string'}])
        # 更新 features 的值。
        features = {'file_upload': {'enabled': False}}
        # 更新 target 的值。
        target = db.session.scalar(select(App).where(App.tenant_id == anchor.tenant_id, App.name == name))
        # 判断条件并选择处理分支。
        if target is None:
            # 更新 dsl 的值。
            dsl = {'version': '0.6.0', 'kind': 'app', 'app': {'name': name, 'description': '登录工作区后点击测试运行。每次任务由已发布的原生 Agent 完整执行；用于与固定工作流对比。', 'mode': 'workflow', 'icon': '🤖', 'icon_background': '#E4FBCC', 'use_icon_as_answer_icon': False},
                   # 执行当前语句并推进处理流程。
                   'dependencies': [], 'workflow': {'graph': graph([start, end]), 'features': features, 'environment_variables': [], 'conversation_variables': []}}
            # 更新 imported 的值。
            imported = AppDslService(db.session).import_app(account=account, import_mode='yaml-content', yaml_content=json.dumps(dsl, ensure_ascii=False))
            # 执行当前语句并推进处理流程。
            assert imported.app_id, str(imported.status)
            # 执行当前语句并推进处理流程。
            db.session.commit()
            # 更新 target 的值。
            target = db.session.get(App, imported.app_id)
        # 执行当前语句并推进处理流程。
        assert target.created_by == account.id and target.name == name
        # 更新 draft 的值。
        draft = db.session.scalar(select(Workflow).where(Workflow.app_id == target.id, Workflow.version == 'draft'))
        # 更新 run 的值。
        run = node('4100000000002', 'agent', label, 420, version='2', agent_node_kind='dify_agent',
                   # 更新 agent_binding 的值。
                   agent_binding={'binding_type': 'roster_agent', 'agent_id': aid},
                   # 更新 agent_task 的值。
                   agent_task='{{#4100000000001.question#}}', agent_declared_outputs=[])
        # 执行当前语句并推进处理流程。
        end['data']['outputs'][0]['value_selector'] = [run['id'], 'text']
        # 执行当前语句并推进处理流程。
        WorkflowService().sync_draft_workflow(app_model=target, graph=graph([start, run, end]), features=features,
            # 更新 unique_hash 的值。
            unique_hash=draft.unique_hash, account=account, environment_variables=[], conversation_variables=[], session=db.session)
        # 更新 published 的值。
        published = WorkflowService().publish_workflow(session=db.session, app_model=target, account=account, marked_name='原生 Agent 对比入口')
        # 执行当前语句并推进处理流程。
        db.session.flush()
        # 更新 target.workflow_id 的值。
        target.workflow_id = published.id
        # 执行当前语句并推进处理流程。
        db.session.commit()
        # 循环处理当前数据。
        for item in [target, db.session.get(App, agent.backing_app_id or agent.app_id)]:
            # 执行当前语句并推进处理流程。
            AppService().update_app_site_status(item, False, session=db.session)
            # 执行当前语句并推进处理流程。
            AppService().update_app_api_status(item, False, session=db.session)
        # 执行当前语句并推进处理流程。
        results.append({'name': name, 'app_id': target.id, 'agent_id': aid, 'workflow_id': published.id})
        # 执行当前语句并推进处理流程。
        print('PUBLISHED', name, target.id, flush=True)
    # 执行当前语句并推进处理流程。
    Path(args.assets, 'workbench-result.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
    # 执行当前语句并推进处理流程。
    print(json.dumps(results, ensure_ascii=False))

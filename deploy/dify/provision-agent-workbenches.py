"""Publish workspace-only, single native-Agent-node comparison entry points."""
import argparse
import json
from pathlib import Path
from app import app
from flask import g
from sqlalchemy import select
from extensions.ext_database import db
from models import Account, Tenant
from models.model import App
from models.workflow import Workflow
from models.agent import Agent
from services.app_dsl_service import AppDslService
from services.app_service import AppService
from services.workflow_service import WorkflowService

parser = argparse.ArgumentParser()
parser.add_argument('--assets', required=True)
parser.add_argument('--anchor-app', required=True)
parser.add_argument('--direct-agent', required=True)
parser.add_argument('--skill-agent', required=True)
args = parser.parse_args()

def node(id, kind, title, x, **data):
    return {'id': id, 'type': 'custom', 'position': {'x': x, 'y': 200},
            'data': {'type': kind, 'title': title, **data}}

def graph(nodes):
    return {'nodes': nodes, 'edges': [
        {'id': f'{a["id"]}-{b["id"]}', 'source': a['id'], 'target': b['id'],
         'sourceHandle': 'source', 'targetHandle': 'target', 'type': 'custom',
         'data': {'sourceType': a['data']['type'], 'targetType': b['data']['type']}}
        for a, b in zip(nodes, nodes[1:])], 'viewport': {'x': 0, 'y': 0, 'zoom': 0.8}}

with app.test_request_context():
    anchor = db.session.get(App, args.anchor_app)
    assert anchor and anchor.name == '炬联 IoT · 运维助手'
    account = db.session.get(Account, anchor.created_by)
    account.set_current_tenant_with_session(db.session.get(Tenant, anchor.tenant_id), session=db.session)
    g._login_user = account
    results = []
    for aid, label in [(args.direct_agent, '原生 Agent'), (args.skill_agent, 'Skills Agent')]:
        agent = db.session.get(Agent, aid)
        assert agent and agent.tenant_id == anchor.tenant_id and agent.created_by == account.id
        name = '炬联 IoT 体验 · ' + label
        start = node('4100000000001', 'start', '对比任务', 80, variables=[
            {'variable': 'question', 'label': '对比任务', 'type': 'paragraph', 'required': True, 'max_length': 32000, 'options': []}])
        end = node('4100000000003', 'end', '分析结果', 760, outputs=[
            {'variable': 'text', 'value_selector': [start['id'], 'question'], 'value_type': 'string'}])
        features = {'file_upload': {'enabled': False}}
        target = db.session.scalar(select(App).where(App.tenant_id == anchor.tenant_id, App.name == name))
        if target is None:
            dsl = {'version': '0.6.0', 'kind': 'app', 'app': {'name': name, 'description': '登录工作区后点击测试运行。每次任务由已发布的原生 Agent 完整执行；用于与固定工作流对比。', 'mode': 'workflow', 'icon': '🤖', 'icon_background': '#E4FBCC', 'use_icon_as_answer_icon': False},
                   'dependencies': [], 'workflow': {'graph': graph([start, end]), 'features': features, 'environment_variables': [], 'conversation_variables': []}}
            imported = AppDslService(db.session).import_app(account=account, import_mode='yaml-content', yaml_content=json.dumps(dsl, ensure_ascii=False))
            assert imported.app_id, str(imported.status)
            db.session.commit()
            target = db.session.get(App, imported.app_id)
        assert target.created_by == account.id and target.name == name
        draft = db.session.scalar(select(Workflow).where(Workflow.app_id == target.id, Workflow.version == 'draft'))
        run = node('4100000000002', 'agent', label, 420, version='2', agent_node_kind='dify_agent',
                   agent_binding={'binding_type': 'roster_agent', 'agent_id': aid},
                   agent_task='{{#4100000000001.question#}}', agent_declared_outputs=[])
        end['data']['outputs'][0]['value_selector'] = [run['id'], 'text']
        WorkflowService().sync_draft_workflow(app_model=target, graph=graph([start, run, end]), features=features,
            unique_hash=draft.unique_hash, account=account, environment_variables=[], conversation_variables=[], session=db.session)
        published = WorkflowService().publish_workflow(session=db.session, app_model=target, account=account, marked_name='原生 Agent 对比入口')
        db.session.flush()
        target.workflow_id = published.id
        db.session.commit()
        for item in [target, db.session.get(App, agent.backing_app_id or agent.app_id)]:
            AppService().update_app_site_status(item, False, session=db.session)
            AppService().update_app_api_status(item, False, session=db.session)
        results.append({'name': name, 'app_id': target.id, 'agent_id': aid, 'workflow_id': published.id})
        print('PUBLISHED', name, target.id, flush=True)
    Path(args.assets, 'workbench-result.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(results, ensure_ascii=False))

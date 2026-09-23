#!/usr/bin/env python3
"""Render the derived feature map from current API/DB/UI; never defines a second domain contract."""
from pathlib import Path
from datetime import datetime,timezone
import json,yaml
root=Path(__file__).resolve().parents[1]
api=yaml.safe_load((root/'03_api_contract_complete.yaml').read_text())
ui=json.loads((root/'contracts/ui_inventory.json').read_text())
db=json.loads((root/'contracts/db_inventory.json').read_text())
ops={op['operationId']:{**op,'path':path,'method':method.upper()} for path,item in api['paths'].items() for method,op in item.items() if isinstance(op,dict) and 'operationId' in op}
tables={f"{t['schema']}.{t['name']}":t for t in db['tables']}
features=[]
for f in sorted(ui['features'],key=lambda f:f['featureId']):
    ids=sorted(set(f.get('operationIds',[]))|{op for op,v in ops.items() if f['featureId'] in v.get('x-feature-id',[])})
    used=set()
    for ident in ids:
        for name in ops[ident].get('x-tables',[]):
            if name=='{owner}.operations':continue
            used.add(name)
    for name,t in tables.items():
        if f['featureId'] in t.get('features',[]):used.add(name)
    documents=['00_master_index.md','01_domain_ownership_matrix.md','02_database_schema_complete.md','03_api_contract_complete.yaml','04_frontend_interaction_spec.md','06_data_flow_and_state_machine.md','07_error_handling_matrix.md','08_delivery_checklist_per_role.md']
    if f['featureId'] in ['F03','F04','F05','F06','F10']:documents.append('05_build_service_technical_spec.md')
    if f['featureId'] in ['F09','F16','F17']:documents.append('09_legacy_migration.md')
    data_owners={ops[x]['x-owner'] for x in ids if ops[x]['x-owner']!='bff'}
    features.append(dict(featureId=f['featureId'],title=f['title'],pages=f['pages'],operationIds=ids,displayFields=f['displayFields'],tables=sorted(used),owners=sorted(data_owners),documents=documents,acceptance=f['scenarios']))
data=dict(schemaVersion=1,generatedFrom=['03_api_contract_complete.yaml','contracts/db_inventory.json','contracts/ui_inventory.json'],authority='derived cross-reference only; edit the relevant API/DB/UI owner, then regenerate',features=features)
(root/'contracts/traceability.json').write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
lines=['# 10 全量功能交付与字段追踪矩阵','','> 自动派生索引，不是第二份产品或字段定义。更新03/02/04后运行 checks/render_traceability.py；随后运行 checks/validate_spec.py。','> 每个F编号贯穿客户场景、页面、后端operationId、DTO字段、持久化Owner与接受标准。通用Operation的BFF入口不是数据Owner；目标由必填owner参数确定。','','## 总表','','| 功能 | 页面 | API操作数 | 数据Owner |','|---|---|---:|---|']
for f in features:
    lines.append(f"| {f['featureId']} {f['title']} | {'、'.join(p['route'] for p in f['pages'])} | {len(f['operationIds'])} | {', '.join(f['owners'])} |")
for f in features:
    lines += ['',f"## {f['featureId']} {f['title']}",'', '**页面**：'+'、'.join(f"`{p['route']}`（{p['title']}）" for p in f['pages']),'','| operationId | 请求 | 请求DTO | 成功响应DTO | 唯一Owner |','|---|---|---|---|---|']
    for ident in f['operationIds']:
        op=ops[ident];body=op.get('requestBody',{}).get('content',{}).get('application/json',{}).get('schema',{})
        req=body.get('$ref','—').split('/')[-1];responses=[]
        for status,response in op['responses'].items():
            if str(status).startswith('2'):
                schema=response.get('content',{}).get('application/json',{}).get('schema',{})
                responses.append(str(status)+' '+schema.get('$ref','无响应体').split('/')[-1])
        owner=op['x-owner'] if op['x-owner']!='bff' else 'BFF入口→请求owner（目标表{owner}.operations）'
        lines.append(f"| `{ident}` | `{op['method']} {op['path']}` | {req} | {'; '.join(responses)} | {owner} |")
    lines += ['','**显示字段来源**（精确Schema.field，包含正常/处理中/失败页字段；可选性按03）：','']
    grouped={}
    for ref in f['displayFields']:
        schema,_,prop=ref.partition('.');grouped.setdefault(schema,[]).append(prop)
    for schema,props in sorted(grouped.items()):lines.append(f"- `{schema}`："+'、'.join(f'`{x}`' for x in sorted(set(props))))
    lines += ['','**数据表及写入Owner**：'+ '、'.join(f'`{t}`' for t in f['tables']),'','**验收向量**：','']
    for a in f['acceptance']:lines.append(f"- **{a['kind']}**：{a['expectation']}")
    lines += ['','跨域状态/恢复执行见06；输入约束、按钮条件和中文错误见04/07。涉及旧对象同时执行09迁移接受标准。']
lines += ['','## 阅读与完成判断','','- 架构师检查本功能Owner与单writer；后端从请求DTO追到表字段/状态与事件；前端从operationId读取同一DTO；产品和客户按验收向量判断结果。','- 金额、Key、真实provider身份等敏感/权威字段的derived projection不是新的持久writer。具体字段来源在03 x-field-map与02说明。','- 矩阵包含的接口不代表当前仓库已实现。实现状态、源码测试、Local/Instance采用必须分别留证。']
(root/'10_feature_traceability.md').write_text('\n'.join(lines)+'\n')
print(f'rendered {len(features)} features, {len(ops)} API operations')

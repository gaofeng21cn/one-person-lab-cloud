from pathlib import Path
import json,re,subprocess,sys,tempfile,hashlib,collections,argparse
import yaml,grpc_tools
from google.protobuf.descriptor_pb2 import FileDescriptorSet,FieldDescriptorProto
P=Path(__file__).resolve().parents[1];R=P.parents[2];OUT=P/'reference';OUT.mkdir(exist_ok=True)
ap=argparse.ArgumentParser(description='Render field-level read views from canonical contracts; never mutates contracts.')
ap.add_argument('--legacy-source-json',type=Path,help='Go AST extraction from the bound upstream snapshot; omit to preserve baseline docs')
ap.add_argument('--report',type=Path,required=True,help='New evidence output, must not already exist')
args=ap.parse_args()
if args.report.exists():raise SystemExit('Refusing to overwrite evidence: '+str(args.report))
api=yaml.safe_load((P/'03_api_contract_complete.yaml').read_text());inv=json.loads((P/'contracts/api_inventory.json').read_text())['operations'];db=json.loads((P/'contracts/db_inventory.json').read_text())['tables'];flows=json.loads((P/'contracts/domain_flows.json').read_text())['flows'];events=json.loads((P/'contracts/events.json').read_text())
owners=['tenant','capability','build','workspace','runtime_control','fabric','gateway','resource_catalog','ledger']
headers={o:(OUT/f'domain-{o}.md').read_text().split('## 2. 客户 REST 与后端 Owner')[0].rstrip().splitlines() for o in owners}
LEGACY='50520e27a6b9a630eefdc2df7da3e5ec498d28a0';SHA='7f5d05fe9b855d8af216caad714cf7fc85014d3c'
def esc(x):return str(x).replace('|','\\|').replace('\n','<br>')
def code(x):return '`'+esc(x)+'`'
def table(headers,rows):return ['| '+' | '.join(headers)+' |','| '+' | '.join('---' for _ in headers)+' |']+['| '+' | '.join(esc(x) for x in row)+' |' for row in rows]
def dump(p,lines):p.write_text('\n'.join(lines)+'\n')
def typ(s):
 if '$ref' in s:return s['$ref'].rsplit('/',1)[-1]
 if s.get('type')=='array':return 'array<'+typ(s['items'])+'>'
 return s.get('type','composition')+(('/'+s['format']) if 'format'in s else '')
def brief(s):
 keys=['enum','const','minimum','maximum','minLength','maxLength','minItems','maxItems','uniqueItems','pattern','nullable','additionalProperties','default','oneOf','anyOf','allOf','discriminator','not']
 return '; '.join(k+'='+json.dumps(s[k],ensure_ascii=False,separators=(',',':')) for k in keys if k in s)
def deref(s):
 while isinstance(s,dict) and '$ref'in s:s=api['components'][s['$ref'].split('/')[2]][s['$ref'].split('/')[3]]
 return s
with tempfile.TemporaryDirectory() as d:
 subprocess.run([sys.executable,'-m','grpc_tools.protoc','-I'+str(P/'contracts'),'-I'+str(Path(grpc_tools.__file__).parent/'_proto'),'--descriptor_set_out='+d+'/p.pb','--include_imports',str(P/'contracts/internal.proto')],check=True,capture_output=True)
 ds=FileDescriptorSet();ds.ParseFromString(Path(d+'/p.pb').read_bytes());proto=next(f for f in ds.file if f.name=='internal.proto')
msgs={m.name:m for m in proto.message_type};rpcs={s.name+'.'+m.name:m for s in proto.service for m in s.method}
def msgname(x):return x.rsplit('.',1)[-1]
def ftype(f):return msgname(f.type_name) if f.type_name else FieldDescriptorProto.Type.Name(f.type).removeprefix('TYPE_').lower()
def fields(m):
 return '；'.join(f'{f.name}#{f.number}: '+('repeated ' if f.label==3 else 'optional ' if f.proto3_optional else '')+ftype(f) for f in msgs[msgname(m)].field) if msgname(m) in msgs else msgname(m)
def mlink(m):n=msgname(m);return f'[{n}](rpc-messages.md#{n.lower()})'
def slink(s):return f'[{s}](rest-schemas.md#{s.lower()})'
# One exact message catalogue; nested fields are linked instead of manufacturing DTO copies.
ls=['# v2.26 RPC 字段目录（派生参考）','','> 来源当前checkout的 `contracts/internal.proto`，通过 protoc descriptor 提取，不是新契约；修改请回到原协议。业务必填/校验不由proto3标量零值自动保证。','',f'{len(rpcs)} 个 RPC，{len(msgs)} 个顶层消息。','## RPC 方法','']
ls+=table(['RPC','请求','响应'],[[code(k),mlink(m.input_type),mlink(m.output_type)] for k,m in rpcs.items()])
for name,m in msgs.items():
 ls+=['',f'## {name}','']
 ls+=table(['字段 #','wire 类型','存在性 / oneof','JSON名'],[[code(f'{f.name} #{f.number}'),mlink(f.type_name) if f.type==11 and msgname(f.type_name) in msgs else code(ftype(f)),('repeated' if f.label==3 else 'proto3 optional' if f.proto3_optional else ('oneof '+m.oneof_decl[f.oneof_index].name if f.HasField('oneof_index') else '标量/message；语义必填见规格')),code(f.json_name)] for f in m.field])
ls+=['','## 枚举（完整数值）','']
for en in proto.enum_type:ls+=['### '+en.name,'', '; '.join(code(v.name+'='+str(v.number)) for v in en.value),'']
dump(OUT/'rpc-messages.md',ls)
# REST schema fields including lineage and derived rules; no heuristic flattening of oneOf.
ls=['# REST DTO 字段目录（派生参考）','','> 来源当前checkout的 `03_api_contract_complete.yaml`。字段来源映射可能是DB直接列、Owner派生或外部读回；同名不表示同权威。','']
for name,s in api['components']['schemas'].items():
 ls+=['',f'## {name}','',f'类型：{code(typ(s))}；Owner：{code(s.get("x-owner","未在此schema单独声明；以操作Owner/来源规则为准"))}；表：{code(s.get("x-table","—"))}',s.get('description',''),'']
 if s.get('properties'):
  ls+=table(['字段','类型','必填','来源 / 转换规则','约束'],[[code(n),slink(x['$ref'].rsplit('/',1)[-1]) if '$ref'in x else code(typ(x)),'是' if n in s.get('required',[]) else '否',json.dumps(s.get('x-field-map',{}).get(n,'未在本schema声明'),ensure_ascii=False),brief(x)] for n,x in s['properties'].items()])
 if brief(s):ls+=['','对象约束：'+code(brief(s))]
 for k,v in s.items():
  if k.startswith('x-') and k not in ['x-owner','x-table','x-field-map']:ls+=['',k+'：'+esc(json.dumps(v,ensure_ascii=False))]
dump(OUT/'rest-schemas.md',ls)
# Owner pages derive facts by current owner, not matching table names to concepts.
for o in owners:
 ops=[x for x in inv if x['owner']==o];tables=[x for x in db if x['owner']==o]
 ls=headers[o]+['','## 2. 客户 REST 与后端 Owner','',f'{len(ops)} 个规格REST操作；浏览器仅经BFF。表中的请求/响应为目标契约，不代表该RPC已挂载。字段展开见DTO目录。','']
 if not ops:ls+=['本Owner没有直接客户REST；通过下方内部RPC受其他Owner调用。这不是“没有API”。']
 for op in ops:
  raw=api['paths'][op['path']][op['method'].lower()]
  ls+=['',f'### {op["operationId"]}', '',code(op['method']+' '+op['path']), '',f'权限：{code(", ".join(op["permissions"]))}；F：{code(", ".join(op["features"]))}；主要成功状态：{code(op["status"])}。']
  params=[deref(x) for x in api['paths'][op['path']].get('parameters',[])+raw.get('parameters',[])]
  if params:ls+=table(['入参位置','字段','类型','必填'],[[p['in'],code(p['name']),code(typ(p['schema'])),'是' if p.get('required') else '否'] for p in params])
  ls+=['','Body：'+(slink(op['request']) if op.get('request') else '无独立命名body，见该操作schema')+'；Response：'+(slink(op['response']) if op.get('response') else '无独立命名response，见该操作schema')]
  for label,key in [('请求','request'),('响应','response')]:
   nm=op.get(key)
   if nm in api['components']['schemas']:
    s=api['components']['schemas'][nm];ls+=['',label+'顶层字段：'+('；'.join(code(k)+': '+typ(v)+('（必填）' if k in s.get('required',[]) else '（可选）') for k,v in s.get('properties',{}).items()) or '组合/引用schema，见上述完整定义。')]
  if op.get('targetOwner'):ls+=['','BFF路由目标：'+code(op['targetOwner'])+'；具体数据Owner由必填owner参数确定，不在BFF持有Operation表。']
  ls+=['','涉及表（规格声明，不自动等于每次都写）：'+', '.join(code(t) for t in op.get('tables',[])), '错误响应：'+', '.join(code(c) for c in raw.get('responses',{}) if c!=str(op['status']))]
 ls+=['','## 3. 本域数据库全字段','',f'`opl_{o}`：{len(tables)} 张表，{sum(len(t["fields"]) for t in tables)} 列。字段权威：[02](../02_database_schema_complete.md)、[SQL](../contracts/schema.sql)；以下从db_inventory派生。**表不是自动等同DDD聚合根**；事务边界见第1节。','']
 for t in tables:
  ls+=['',f'### {t["schema"]}.{t["name"]}', '',t.get('purpose',''),'']
  ls+=table(['列','SQL类型','可NULL','默认值','API血缘 / 原始来源'],[[code(f['name']),code(f['type']),'是' if f['nullable'] else '否',code(f['default'] if f['default'] is not None else '—'),'; '.join(f.get('apiLineage',[]) or [f.get('source','未定义')])] for f in t['fields']])
  ls+=['','约束：']+['- '+code(c) for c in t.get('constraints',[])]+['','索引：']+['- '+code(json.dumps(x,ensure_ascii=False)) for x in t.get('indexes',[])]
 serviceOwners={
 'TenantProductService':['tenant'],'CapabilityProductService':['capability'],'BuildProductService':['build'],
 'ResourceCatalogProductService':['resource_catalog'],'WorkspaceProductService':['workspace'],'GatewayProductService':['gateway'],'LedgerProductService':['ledger'],
 'CapabilityCoordination':['capability'],'BuildCoordination':['build'],'CloudIdentityAuthorization':['tenant'],
 'CatalogCoordination':['resource_catalog'],'WorkspaceAdmission':['workspace'],'GatewayCoordination':['gateway'],
 'FabricCoordination':['fabric'],'RuntimeCoordination':['runtime_control'],'FabricRuntimeExecution':['fabric'],'FabricRouteExecution':['fabric'],
 'TenantWorkspaceCoordination':['workspace'],'LedgerCoordination':['ledger'],'WorkspacePlanChangeReadback':['workspace'],
 'FabricPlanTransitionReadback':['fabric'],'GatewayPlanChangeSettlement':['gateway'],'RuntimePlanChangeControl':['runtime_control'],'LedgerPlanChangeEvidence':['ledger'],
 'ClaimUsageReadback':['build','workspace'],'OwnerOperations':[x for x in owners if x!='ledger'],
 'OwnerCommitReadback':[x for x in owners if x!='ledger'],'WorkspaceAuthorizationReadback':['workspace'],'DomainInbox':owners}
 if set(serviceOwners)!=set(s.name for s in proto.service):raise ValueError('RPC service inventory changed; review owner bindings explicitly')
 ls+=['','### 本Owner应实现的RPC方法全集（目标，不是挂载证据）','', '共享OwnerOperations/CommitReadback/Inbox分别由本域实现，不形成中央业务服务；通用Operation REST由BFF按显式Owner路由；本域只负责自己的Operation与授权。','']
 ls+=table(['RPC','请求','响应'],[[code(k),mlink(m.input_type),mlink(m.output_type)] for k,m in rpcs.items() if o in serviceOwners[k.split('.')[0]]])
 ls+=['','## 4. 跨域调用：调用者 → 拥有方 → 字段 → 结果','', '以下只列`domain_flows.json`声明的业务边；共享通道/尚无业务边的RPC不能推断成已经实现。','']
 for fl in flows:
  for i,st in enumerate(fl['steps'],1):
   if st['owner']!=o and st['caller']!=o:continue
   ls+=['',f'### {fl["featureId"]}.{i} {st["caller"]} → {st["owner"]} / {st["rpc"]}','',st['when'],'','请求 '+mlink(st['request'])+'：'+fields(st['request']),'','返回 '+mlink(st['response'])+'：'+fields(st['response']),'','接收方写入：'+', '.join(code(t) for t in st['writes']),'','完成证据：'+st['completionEvidence'],'','失败/未知：'+st['onUnknownOrRejection']]
 ls+=['','## 5. 事件：谁生产、谁消费、哪些字段','', 'aggregate_type由事件精确版本的x-aggregate-identity.type派生；aggregateId须与其idPayloadField一致。revision由生产者聚合事务内分配；consumer_owner显式选择本域Inbox。字段与实现状态不得混同。','']
 for b in events['oneOf']:
  pr=b['properties'];producer=pr['owner']['const'];cs=b['x-consumers']
  if producer!=o and o not in cs:continue
  ref=pr['payload']['$ref'].split('/')[-1];payload=events['$defs'][ref]
  identity=b['x-aggregate-identity']
  ls+=['',f'### {pr["eventType"]["const"]}', '',f'`{producer}` → '+', '.join(code(c) for c in cs),'', '聚合类型：'+code(identity['type'])+'；ID来源：'+code('payload.'+identity['idPayloadField'])+'（须等于aggregateId）','', 'Envelope字段：'+', '.join(code(k) for k in pr),'']
  ls+=table(['payload字段','类型','必填','约束'],[[code(k),code(typ(v)),'是' if k in payload.get('required',[]) else '否',brief(v)] for k,v in payload['properties'].items()])
  ls+=['',payload.get('description',''),b.get('description','')]
 dump(OUT/f'domain-{o}.md',ls)
# Frozen old source extraction plus actual fresh DB schema. Keep source refs, no claimed runtime DTO synthesis.
schema=json.loads((P/'checks/runs/legacy-schema-audit-20260923.json').read_text())
if args.legacy_source_json:
 legacy=json.loads(args.legacy_source_json.read_text());schema=json.loads((P/'checks/runs/legacy-schema-audit-20260923.json').read_text())
 for o in ['control-plane','fabric','ledger']:
  d=legacy[o];s=schema['schemas'][o]
  ls=[f'# 继承基线：{o} 的实际 API / DB','',f'> 上游与fork共同基线 `{LEGACY}`。API由Go AST的真实注册函数路径提取；DB为临时空PostgreSQL执行真实loader后的readback，不是生产数据库/历史数据迁移结果。fork `{SHA}` 保留此模块业务代码。','', '## 覆盖与判读','',f'{len(d["routes"])} 个已挂载路由pattern（含健康/静态/代理/明确404入口，不等同业务API数量）；{len(set(c["table_name"] for c in s["columns"]))} 张实际表、{len(s["columns"])} 列（含迁移journal）。','', '请求/响应中的map和helper不做猜测式OpenAPI转换。每条附实际handler源码及其调用点；命名JSON类型完整字段见[旧DTO目录](legacy-wire-types.md)，动态投影以源handler/decoder为准。租户校验、middleware和数据库状态影响响应，不能从字段存在推导授权。','', '## API注册清单','']
  ls+=table(['pattern','注册函数','源码'],[[code(r['pattern']),code(r['registration']),code(r['source'])] for r in d['routes']])
  ls+=['','## 每条API的实际字段处理与交互','']
  for i,r in enumerate(d['routes'],1):
   ls+=['',f'### {i}. {r["pattern"]}','',f'注册：`{r["source"]}`；`{r["registration"]}`。','', '实际调用：'+', '.join(code(c) for c in r['calls']), '', '<details><summary>展开当前handler：请求解码、字段、权限包装、响应与委托</summary>','','```go',r['handler'],'```','','</details>']
   if r.get('inputs'):
    ls+=['','直接可见的请求字段/解码变量（委托helper中的字段仍以上述真实handler为准）：','']
    unique={(f['name'],f['type'],f.get('tag','')) for f in r['inputs']}
    ls+=table(['字段/变量','类型','来源'],[[code(n),code(t),origin] for n,t,origin in sorted(unique)])
   if r.get('outputs'):
    ls+=['','响应构造（含所有可见分支；不是承诺每次返回同一形状）：','']+['- '+code(x) for x in sorted(set(r['outputs']))]
   links=[fn for fn in d['functions'] if fn['name'] in r['calls']]
   if links:ls+=['','委托定义（须继续遵循该decoder/投影，不按名称猜字段）：'+ '; '.join(code(fn['name'])+' @ '+code(fn['source']) for fn in links)]
  ls+=['','## 数据库实际字段与约束','']
  for t in sorted(set(c['table_name'] for c in s['columns'])):
   ls+=['',f'### {t}','']
   ls+=table(['列','PostgreSQL类型','可NULL','默认值','长度/精度'],[[code(c['column_name']),code(c['udt_name']),c['is_nullable'],code(c['column_default'] or '—'),str(c['character_maximum_length'] or c['numeric_precision'] or '—')] for c in s['columns'] if c['table_name']==t])
   ls+=['','约束：']+['- '+code(c['name'])+' '+code(c['definition']) for c in s['constraints'] if c['table_name']==t]
   ls+=['','索引：']+['- '+code(c['indexdef']) for c in s['indexes'] if c['tablename']==t]
  ls+=['','## 未自动提升为完整性的项目','', '- 空库安装结果不证明已有客户数据的迁移、归档表在特定旧版本下的全部形状。','- Go AST清单不是网络扫描；未登录、越权、retired guard、静态/应用host分流仍由真实入口决定。','- 关联对象和JSON文本必须使用其真实typed decoder；不把HTTP DTO等同数据库实体。']
  if d['unresolved']:ls+=['- 未解析注册：'+str(d['unresolved'])]
  dump(OUT/f'legacy-{o}.md',ls)
 ls=['# 继承实现的 JSON 类型字段','',f'> 从共同基线 `{LEGACY}` 的非生成Go源码AST提取。原始JSON tag完整保留；omitempty不是业务可选/授权声明。嵌入结构、别名和map的细节按真实类型/decoder，不新增平行schema。','']
 for o,d in legacy.items():
  ls+=['',f'## {o}','']
  for t in d['types']:
   ls+=['',f'### {t["name"]}', '',code(t['source']),'',('别名 → '+code(t['alias'])) if t.get('alias') else '']+table(['Go字段','类型','JSON/其他tag'],[[code(f['name'] or '(embedded)'),code(f['type']),code(f.get('tag',''))] for f in t['fields']])
 dump(OUT/'legacy-wire-types.md',ls)
# BFF is an entry adapter, not another domain. Keep its routing read view in the existing REST catalogue.
ls=(OUT/'rest-schemas.md').read_text().rstrip().splitlines()
ls+=['', '## BFF通用Operation路由（派生参考）', '', '> 来源03与api_inventory；仅说明入口寻址，不建立第十个业务数据库。', '']
for op in inv:
 if op['owner']!='bff':continue
 ls+=['',f'### {op["operationId"]}','',code(op['method']+' '+op['path']),'','目标Owner：'+code(op['targetOwner'])+'；权限：'+code(', '.join(op['permissions'])),'','表：'+', '.join(code(t) for t in op['tables'])+'（读写均在目标Owner，非BFF）。','请求：'+code(op.get('request') or '无')+'；响应：'+code(op.get('response') or '无')]
dump(OUT/'rest-schemas.md',ls)
# Summary report to enable exact count and content hash validation.
files=[OUT/f'domain-{o}.md' for o in owners]+[OUT/x for x in ['rpc-messages.md','rest-schemas.md','legacy-control-plane.md','legacy-fabric.md','legacy-ledger.md','legacy-wire-types.md']]
report={'sourceContractHashes':{f:hashlib.sha256((P/f).read_bytes()).hexdigest() for f in ['03_api_contract_complete.yaml','contracts/internal.proto','contracts/events.json','contracts/db_inventory.json','contracts/api_inventory.json','contracts/domain_flows.json']},'baselineSHA':LEGACY,'forkSHA':SHA,'counts':{'restOperations':len(inv),'tables':len(db),'columns':sum(len(t['fields']) for t in db),'rpcMethods':len(rpcs),'messages':len(msgs),'events':len(events['oneOf']),'flowSteps':sum(len(f['steps']) for f in flows)},'byOwner':{o:{'api':sum(x['owner']==o for x in inv),'tables':sum(x['owner']==o for x in db),'columns':sum(len(x['fields']) for x in db if x['owner']==o)} for o in owners},'legacy':{o:{'routes':len(schema['routePatterns'][o]),'tables':len(set(c['table_name'] for c in schema['schemas'][o]['columns'])),'columns':len(schema['schemas'][o]['columns'])} for o in ['control-plane','fabric','ledger']},'outputs':{str(f.relative_to(P)):hashlib.sha256(f.read_bytes()).hexdigest() for f in files}}
args.report.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report['counts'],ensure_ascii=False));print('rendered',len(files),'reference documents')

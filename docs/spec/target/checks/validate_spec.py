#!/usr/bin/env python3
"""Offline specification checks; no database/provider access or product mutation."""
from pathlib import Path
from datetime import datetime, timezone
import hashlib, importlib.metadata, json, re, subprocess, sys, tempfile
import yaml
from jsonschema import RefResolver
from openapi_schema_validator import OAS30Validator
from openapi_spec_validator import validate_spec
from pglast import parser
ROOT=Path(__file__).resolve().parents[1]
errors,checks=[],[]
def require(ok,message):
    if not ok: errors.append(message)
def check(name,fn):
    before=len(errors)
    try: result=fn()
    except Exception as e: errors.append(f'{name}: {type(e).__name__}: {e}');result=None
    checks.append(dict(name=name,passed=len(errors)==before,details=result))
class UniqueLoader(yaml.SafeLoader):pass
def unique_mapping(loader,node,deep=False):
    result={}
    for k,v in node.value:
        key=loader.construct_object(k,deep=deep)
        if key in result:raise ValueError(f'duplicate YAML key {key!r}')
        result[key]=loader.construct_object(v,deep=deep)
    return result
UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG,unique_mapping)
api=yaml.load((ROOT/'03_api_contract_complete.yaml').read_text(),Loader=UniqueLoader)
schemas=api['components']['schemas']
db=json.loads((ROOT/'contracts/db_inventory.json').read_text())
ui=json.loads((ROOT/'contracts/ui_inventory.json').read_text())
inv=json.loads((ROOT/'contracts/api_inventory.json').read_text())
operations={}
for path,item in api['paths'].items():
    for method,op in item.items():
        if not isinstance(op,dict) or 'operationId' not in op:continue
        ident=op['operationId'];require(ident not in operations,f'duplicate operationId {ident}')
        operations[ident]={**op,'path':path,'method':method.upper()}
tables={f"{t['schema']}.{t['name']}":t for t in db['tables']}
columns={f"{key}.{f['name']}":f for key,t in tables.items() for f in t['fields']}
FEATURES={f'F{i:02d}' for i in range(1,18)}
def resolve(ref):
    if not ref.startswith('#/'):raise ValueError(f'external ref: {ref}')
    obj=api
    for p in ref[2:].split('/'):obj=obj[p.replace('~1','/').replace('~0','~')]
    return obj

def openapi_check():
    validate_spec(api)
    for name,op in operations.items():
        require(op['path'].startswith('/api/v2/'),f'wrong BFF path: {name}')
        for k in ['x-owner','x-feature-id','x-permission']:require(bool(op.get(k)),f'{k} missing: {name}')
        for f in op.get('x-feature-id',[]):require(f in FEATURES,f'unknown feature {f}: {name}')
        params=[resolve(x['$ref']) if '$ref' in x else x for x in op.get('parameters',[])]
        names={x['name'] for x in params}
        for p in re.findall(r'\{([^}]+)\}',op['path']):require(p in names,f'path param {p} missing: {name}')
        if op['method'] in {'POST','PATCH','PUT','DELETE'}:
            require('Idempotency-Key' in names,f'write without idempotency: {name}')
            require(any('csrf' in x.lower() for x in names),f'write without CSRF: {name}')
        for t in op.get('x-tables',[]):
            if t=='{owner}.operations':
                for owner in schemas['OperationOwner']['enum']:require(f'{owner}.operations' in tables,f'missing operations: {owner}')
            else:require(t in tables,f'API table missing: {name}->{t}')
    old={x['operationId']:x for x in inv['operations']}
    require(set(old)==set(operations),'API inventory operation set differs')
    for name,op in operations.items():
        if name not in old:continue
        for k in ['path','method']:require(old[name][k]==op[k],f'API inventory {k} differs: {name}')
        require(old[name]['features']==op['x-feature-id'],f'API inventory features differ: {name}')
        require(old[name]['owner']==op['x-owner'],f'API inventory owner differs: {name}')
    return dict(operations=len(operations),schemas=len(schemas))

def sql_check():
    text=(ROOT/'contracts/schema.sql').read_text();ast=json.loads(parser.parse_sql_json(text))
    actual,indexes={},{}
    for item in ast['stmts']:
        stmt=item['stmt']
        if 'CreateStmt' in stmt:
            node=stmt['CreateStmt'];rel=node['relation'];key=f"{rel['schemaname']}.{rel['relname']}";fields={}
            for elt in node.get('tableElts',[]):
                if 'ColumnDef' in elt:
                    f=elt['ColumnDef'];kinds={c['Constraint']['contype'] for c in f.get('constraints',[])}
                    typ=f['typeName']['names'][-1]['String']['sval'] + ('[]' if f['typeName'].get('arrayBounds') else '')
                    fields[f['colname']]=dict(nullable='CONSTR_NOTNULL' not in kinds,type=typ)
                if 'Constraint' in elt and elt['Constraint']['contype']=='CONSTR_FOREIGN':
                    c=elt['Constraint'];target=c['pktable'];schema=target.get('schemaname',rel['schemaname']);t=f"{schema}.{target['relname']}"
                    require(schema==rel['schemaname'],f'cross-owner FK: {key}->{t}')
                    require(t in tables,f'FK target missing: {t}')
                    if t in tables:
                        cols={f['name'] for f in tables[t]['fields']}
                        for col in c.get('pk_attrs',[]):require(col['String']['sval'] in cols,f'FK column absent: {t}')
            actual[key]=fields
        if 'IndexStmt' in stmt:
            n=stmt['IndexStmt'];indexes[f"{n['relation']['schemaname']}.{n['idxname']}"]=n
    require(set(actual)==set(tables),f'SQL/inventory table set differs: {set(actual)^set(tables)}')
    aliases={'bigint':'int8','integer':'int4','int':'int4','boolean':'bool','timestamp with time zone':'timestamptz'}
    for key,t in tables.items():
        if key not in actual:continue
        expected={f['name']:f for f in t['fields']}
        require(set(expected)==set(actual[key]),f'SQL/inventory columns differ: {key}')
        for name,f in expected.items():
            if name not in actual[key]:continue
            require(f['nullable']==actual[key][name]['nullable'],f'nullable mismatch: {key}.{name}')
            typ=aliases.get(f['type'],f['type'])
            require(typ==actual[key][name]['type'],f'type mismatch: {key}.{name}: {typ}/{actual[key][name]["type"]}')
        for ix in t.get('indexes',[]):require(f"{t['schema']}.{ix['name']}" in indexes,f'SQL index missing: {key}.{ix["name"]}')
    for d in db['databases']:require(f"-- BEGIN DATABASE {d['name']}" in text and f"current_database() <> '{d['name']}'" in text,f'DB guard missing: {d["name"]}')
    return dict(databases=len(db['databases']),tables=len(actual),columns=sum(map(len,actual.values())),indexes=len(indexes),sqlStatements=len(ast['stmts']),execution='parser only; no database connected')

def field_map_check():
    persisted=derived=0
    for name,s in schemas.items():
        mapping=s.get('x-field-map',{})
        if not mapping:continue
        for prop,source in mapping.items():
            require(prop in s.get('properties',{}),f'mapped property missing: {name}.{prop}')
            if isinstance(source,str) and source in columns:persisted+=1
            elif isinstance(source,str) and source.startswith('derived:'):derived+=1
            elif isinstance(source,dict) and ('derived' in source or 'sources' in source):
                derived+=1
                require(bool(source.get('rule')),f'derived rule absent: {name}.{prop}')
                for origin in source.get('sources',[]):require(origin in columns,f'derived source column absent: {name}.{prop}->{origin}')
            else:errors.append(f'field source invalid: {name}.{prop}->{source}')
        require(set(mapping)==set(s.get('properties',{})),f'field map incomplete: {name}')
    return dict(persistedFieldMappings=persisted,derivedFieldMappings=derived)

def ui_check():
    seen=set();fields=actions=0
    for f in ui['features']:
        fid=f['featureId'];seen.add(fid)
        require(bool(f.get('pages')),f'UI pages missing: {fid}');require(bool(f.get('displayFields')),f'UI fields missing: {fid}')
        for oid in f.get('operationIds',[]):require(oid in operations,f'UI unknown operation: {fid}/{oid}')
        for ref in f.get('displayFields',[]):
            fields+=1;schema,_,prop=ref.partition('.')
            require(schema in schemas,f'UI unknown schema: {ref}')
            if schema in schemas:require(prop in schemas[schema].get('properties',{}),f'UI field absent: {ref}')
        for a in f.get('actions',[]):
            actions+=1;oid=a['operationId'];require(oid in operations,f'UI action absent: {fid}/{oid}')
            require(oid in f['operationIds'],f'UI action not listed: {fid}/{oid}');require(bool(a.get('condition')),f'UI action no condition: {fid}/{oid}')
        require(len({s['kind'] for s in f.get('scenarios',[])})>=5,f'UI test vectors incomplete: {fid}')
    require(seen==FEATURES,f'UI feature coverage missing: {FEATURES-seen}')
    return dict(features=len(seen),uiFieldReferences=fields,actions=actions)

def trace_check():
    trace=json.loads((ROOT/'contracts/traceability.json').read_text());require({f['featureId'] for f in trace['features']}==FEATURES,'traceability incomplete')
    by_ui={f['featureId']:f for f in ui['features']}
    for f in trace['features']:
        require(set(f['displayFields'])==set(by_ui[f['featureId']]['displayFields']),f'trace/UI display fields differ: {f["featureId"]}')
        require(bool(f['tables']),f'trace tables missing: {f["featureId"]}')
        for t in f['tables']:require(t in tables,f'trace table absent: {t}')
        for op in f['operationIds']:require(op in operations,f'trace operation absent: {op}')
        for p in f['documents']:require((ROOT/p).is_file(),f'trace document absent: {p}')
    return dict(features=len(trace['features']))

def proto_check():
    import grpc_tools
    proto=ROOT/'contracts/internal.proto';require(proto.is_file(),'internal.proto absent')
    with tempfile.TemporaryDirectory(prefix='opl-proto-spec-') as temp:
        include=Path(grpc_tools.__file__).parent/'_proto'
        r=subprocess.run([sys.executable,'-m','grpc_tools.protoc',f'-I{ROOT/"contracts"}',f'-I{include}',f'--descriptor_set_out={temp}/spec.pb','--include_imports',str(proto)],capture_output=True,text=True)
        require(r.returncode==0,'protobuf compile failed: '+r.stderr)
        if r.returncode:return
        from google.protobuf.descriptor_pb2 import FileDescriptorSet
        d=FileDescriptorSet();d.ParseFromString(Path(temp,'spec.pb').read_bytes());files=[f for f in d.file if f.name=='internal.proto']
        require(len(files)==1,'own proto descriptor absent')
        return dict(services=sum(len(f.service) for f in files),messages=sum(len(f.message_type) for f in files),methods=sum(len(s.method) for f in files for s in f.service),compilerWarnings=r.stderr.strip())

def events_check():
    from jsonschema.validators import validator_for
    data=json.loads((ROOT/'contracts/events.json').read_text());validator_for(data).check_schema(data)
    items=data['oneOf'];require(bool(items),'typed events absent');names=[]
    for item in items:
        name=item['properties']['eventType']['const'];names.append(name)
        owner=item['properties']['owner']['const'];require(bool(owner),f'event producer absent: {name}')
        payload=item['properties']['payload']['$ref'];require(payload.startswith('#/$defs/'),f'event payload ref wrong: {name}')
        require(payload.split('/')[-1] in data['$defs'],f'event payload absent: {name}')
        require(bool(item.get('x-consumers')),f'event consumers absent: {name}')
        require(item.get('additionalProperties') is False,f'permissive event envelope: {name}')
    require(len(names)==len(set(names)),'duplicate event identity')
    require('Outbox' in data['description'] and 'Inbox' in data['description'],'event transaction semantics absent')
    validator=validator_for(data)(data)
    cases=json.loads((ROOT/'checks/event_cases.json').read_text())['cases']
    for c in cases:
        valid=not list(validator.iter_errors(c['value']))
        require(valid==c['valid'],f'event example {c["name"]}: expected {c["valid"]}, got {valid}')
    return dict(events=len(items),payloadSchemas=len(data['$defs']),explicitPositiveNegativeCases=len(cases))

def semantic_cases():
    cases=json.loads((ROOT/'checks/contract_cases.json').read_text())['cases'];resolver=RefResolver.from_schema(api)
    for c in cases:
        issues=list(OAS30Validator(schemas[c['schema']],resolver=resolver).iter_errors(c['value']));valid=not issues
        require(valid==c['valid'],f'counterexample {c["name"]}: expected valid={c["valid"]}, got {valid}: {[e.message for e in issues[:2]]}')
    return dict(explicitPositiveNegativeCases=len(cases))

def database_execution_receipt():
    candidates=sorted((ROOT/'checks/runs').glob('cross-domain-*.json'))
    require(bool(candidates),'cross-domain database receipt absent')
    receipt=json.loads(candidates[-1].read_text())
    for source,digest in receipt['sourceHashes'].items():
        require(hashlib.sha256((ROOT/source).read_bytes()).hexdigest()==digest,f'cross-domain executed source hash differs: {source}')
    require(receipt['passed'] is True,'latest cross-domain execution receipt failed')
    require(receipt['sourceUnchangedDuringRun'] is True,'contracts drifted during cross-domain validation')
    require(all(c['passed'] for c in receipt['cases']),'cross-domain semantic case failed')
    require(receipt['cleanup']['containerAbsent'] is True,'temporary DB cleanup not confirmed')
    return dict(receipt=str(candidates[-1].relative_to(ROOT)),tests=len(receipt['cases']),metadata=receipt['metadata'],scope=receipt['scope'],cleanup=receipt['cleanup'])

def error_state_projection_check():
    text=(ROOT/'07_error_handling_matrix.md').read_text()
    error_codes=schemas['ErrorCode']['enum']
    stages=schemas['OperationStage']['enum']
    for code in error_codes:require(f'`{code}`' in text,f'error UI projection missing: {code}')
    stage_text=(ROOT/'07_error_handling_matrix.md').read_text()
    for stage in stages:require(f'`{stage}`' in stage_text,f'stage UI projection missing: {stage}')
    require(set(inv['errorCodes'])==set(error_codes),'error inventory differs from API')
    return dict(errorCodes=len(error_codes),operationStages=len(stages))

check('openapi_and_operation_inventory',openapi_check)
check('sql_ast_database_boundary_and_inventory',sql_check)
check('isolated_postgresql_execution_receipt',database_execution_receipt)
check('api_database_field_sources',field_map_check)
check('ui_fields_actions_feature_coverage',ui_check)
check('error_and_stage_projections',error_state_projection_check)
check('feature_traceability',trace_check)
check('protobuf_compilation',proto_check)
check('event_payload_schemas',events_check)
check('contract_positive_negative_cases',semantic_cases)
now=datetime.now(timezone.utc)
report=dict(schemaVersion=1,timestamp=now.isoformat(),result='passed' if not errors else 'failed',scope='documentation contracts only; not implementation/runtime/DB execution/Instance qualification',checks=checks,errors=errors,toolVersions={p:importlib.metadata.version(p) for p in ['openapi-spec-validator','grpcio-tools','pglast','PyYAML','jsonschema']},artifacts={str(f.relative_to(ROOT)):hashlib.sha256(f.read_bytes()).hexdigest() for f in sorted(ROOT.rglob('*')) if f.is_file() and 'checks/runs/' not in str(f.relative_to(ROOT)) and f.name not in {'verification.json','.DS_Store'} and '__MACOSX' not in f.parts and '__pycache__' not in f.parts and f.suffix!='.pyc'})
runs=ROOT/'checks/runs';runs.mkdir(exist_ok=True);path=runs/(now.strftime('%Y%m%dT%H%M%S%fZ')+'.json')
path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
(ROOT/'checks/verification.json').write_text(json.dumps(dict(result=report['result'],timestamp=report['timestamp'],report=str(path.relative_to(ROOT)),scope=report['scope'],checks=checks,errors=errors),ensure_ascii=False,indent=2)+'\n')
print(json.dumps(dict(result=report['result'],checks=checks,errors=errors,receipt=str(path)),ensure_ascii=False,indent=2));sys.exit(bool(errors))

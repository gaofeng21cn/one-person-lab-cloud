#!/usr/bin/env python3
"""Executable specification adapters + isolated PostgreSQL tests, NOT product-service E2E.

Uses only the already-present postgres:16-alpine image, --network none, tmpfs and
fresh databases. No provider/Gateway calls, credentials, host ports or host mounts.
All receipts bind exact input hashes; a changed source during the run is a failure.
"""
from pathlib import Path
from datetime import datetime, timezone
import argparse, calendar, copy, hashlib, importlib, json, re, subprocess, sys, tempfile, time
from concurrent.futures import ThreadPoolExecutor
import yaml
from jsonschema import RefResolver, Draft202012Validator
from openapi_schema_validator import OAS30Validator
ROOT=Path(__file__).resolve().parents[1]
SOURCE_FILES=['03_api_contract_complete.yaml','contracts/schema.sql','contracts/db_inventory.json','contracts/internal.proto','contracts/publisher-contract.schema.json','checks/semantics_cases.json','checks/validate_cross_domain.py','13_plan_change_policy.md','contracts/plan-change-policy.json']
API=yaml.safe_load((ROOT/SOURCE_FILES[0]).read_text()); SCHEMAS=API['components']['schemas']
INVENTORY=json.loads((ROOT/'contracts/db_inventory.json').read_text())
PUBLISHER=json.loads((ROOT/'contracts/publisher-contract.schema.json').read_text())
CASES=json.loads((ROOT/'checks/semantics_cases.json').read_text())['cases']
PLAN_POLICY=json.loads((ROOT/'contracts/plan-change-policy.json').read_text())['x-policy']
RESOLVER=RefResolver.from_schema(API)
MAX_INT64=9223372036854775807
NOW='2026-09-21T12:00:00Z'
class Rejection(Exception):
 def __init__(self,layer,reason): super().__init__(reason);self.layer=layer
class PgError(Exception):
 def __init__(self,state,text): super().__init__(text);self.state=state

def q(v):
 if v is None:return 'NULL'
 if isinstance(v,bool):return 'TRUE' if v else 'FALSE'
 if isinstance(v,int):return str(v)
 if isinstance(v,(bytes,bytearray)):return "decode("+q(bytes(v).hex())+",'hex')"
 return "'"+str(v).replace("'","''")+"'"
def j(v):return q(json.dumps(v,ensure_ascii=False,separators=(',',':')))+'::jsonb'
def digest(b):return 'sha256:'+hashlib.sha256(b).hexdigest()
def api_validate(name,value):
 errors=list(OAS30Validator(SCHEMAS[name],resolver=RESOLVER).iter_errors(value))
 if errors:raise Rejection('api_reject',name+': '+errors[0].message)
def money(v):
 if not isinstance(v,str) or not re.fullmatch(r'0|[1-9][0-9]*',v):raise Rejection('owner_reject','non-canonical integer money')
 n=int(v)
 if n>MAX_INT64:raise Rejection('owner_reject','int64 overflow')
 return n
def owner_assert(ok,msg):
 if not ok:raise Rejection('owner_reject',msg)
def sql_insert(table,data):
 values=[j(v) if isinstance(v,(dict,list)) else q(v) for v in data.values()]
 return 'INSERT INTO '+table+' ('+','.join(data)+') VALUES ('+','.join(values)+');'

def protocol_module(directory):
 import grpc_tools
 p=subprocess.run([sys.executable,'-m','grpc_tools.protoc','-I',str(ROOT/'contracts'),'-I',str(Path(grpc_tools.__file__).parent/'_proto'),'--python_out='+str(directory),str(ROOT/'contracts/internal.proto')],capture_output=True,text=True)
 if p.returncode:raise RuntimeError('protobuf compile failed: '+p.stderr)
 sys.path.insert(0,str(directory));return importlib.import_module('internal_pb2')

def proto_validate(pb,name,value):
 from google.protobuf.json_format import ParseDict
 try:return ParseDict(value,getattr(pb,name)(),ignore_unknown_fields=False)
 except Exception as e:raise Rejection('decoder_reject',name+': '+str(e))

class Database:
 def __init__(self,name):self.name=name;self.sql_count=0
 def sql(self,owner,statement):
  db=owner if owner=='postgres' else 'opl_'+owner
  p=subprocess.run(['docker','exec','-i',self.name,'psql','-h','127.0.0.1','-q','-X','-v','ON_ERROR_STOP=1','-v','VERBOSITY=verbose','-U','postgres','-d',db,'-At'],input=statement,capture_output=True,text=True)
  self.sql_count+=1
  if p.returncode:
   m=re.search(r'ERROR:\s+([A-Z0-9]{5}):',p.stderr)
   raise PgError(m[1] if m else 'UNKNOWN',p.stderr)
  return p.stdout.strip()
 def insert(self,owner,table,**data):return self.sql(owner,sql_insert(owner+'.'+table,data))
 def one_json(self,owner,query):
  result=self.sql(owner,'SELECT row_to_json(t) FROM ('+query+') t;')
  return json.loads(result) if result else None
 def setup(self):
  text=(ROOT/'contracts/schema.sql').read_text()
  for spec in INVENTORY['databases']:
   db=spec['name'];self.sql('postgres',f'CREATE ROLE {db}_owner NOLOGIN; CREATE ROLE {db}_writer NOLOGIN; CREATE DATABASE {db};')
   block=re.search(r'-- BEGIN DATABASE '+re.escape(db)+r'\n(.*?)-- END DATABASE '+re.escape(db),text,re.S).group(1)
   self.sql(spec['owner'],block)
 def verify_inventory(self):
  expected_types={'text':'text','bigint':'int8','integer':'int4','timestamptz':'timestamptz','boolean':'bool','jsonb':'jsonb','text[]':'_text','bytea':'bytea'}
  count=0
  for d in INVENTORY['databases']:
   o=d['owner'];raw=self.sql(o,"SELECT table_name||'|'||column_name||'|'||udt_name||'|'||is_nullable FROM information_schema.columns WHERE table_schema="+q(o)+';')
   actual={tuple(r.split('|')[:2]):r.split('|')[2:] for r in raw.splitlines()}
   for t in [t for t in INVENTORY['tables'] if t['owner']==o]:
    for f in t['fields']:
     assert actual[(t['name'],f['name'])]==[expected_types[f['type']],'YES' if f['nullable'] else 'NO'],(o,t['name'],f['name'])
     count+=1
  return count

def seed_workspace(db,ident,kind='agent_saas',epoch=2):
 db.insert('workspace','workspaces',id=ident,tenant_id='test-tenant',name=ident,status='active',capability_version_id='test-capability' if kind!='legacy_resource_only' else None,compute_plan_id='test-compute',storage_plan_id='test-storage',created_by='test-actor',delivery_model=kind,version=1)

def seed_catalog(db):
 db.sql('resource_catalog',"INSERT INTO resource_catalog.compute_plans(id,name,version_label,provider,provider_profile_ref,region,status,billing_mode,valid_from,published_by,provider_capability_version,provider_specification,vcpus,memory_mib) VALUES ('test-compute','compute','v1','local','test-profile','local','approved','LOCAL_NO_CHARGE','2026-01-01','test-admin','provider/v1','{}',1,1024);")
 db.sql('resource_catalog',"INSERT INTO resource_catalog.storage_plans(id,name,version_label,provider,provider_profile_ref,region,status,billing_mode,valid_from,published_by,provider_capability_version,provider_specification,capacity_gib,shrink_supported) VALUES ('test-storage','storage','v1','local','test-profile','local','approved','LOCAL_NO_CHARGE','2026-01-01','test-admin','provider/v1','{}',1,false);")
 db.sql('resource_catalog',"INSERT INTO resource_catalog.retention_policy_versions(id,version_label,rules,customer_terms,valid_from,published_by,workspace_data_disposition,package_history_disposition,build_history_disposition,tenant_restore_days) VALUES ('test-retention','v1','{}','test-only','2026-01-01','test-admin','destroy_after_confirmed_deletion','retain','retain',15); INSERT INTO resource_catalog.refund_policy_versions(id,version_label,rules,customer_terms,valid_from,published_by,algorithm,retention_policy_version_id) VALUES ('test-refund','v1','{}','test-only','2026-01-01','test-admin','workspace-delete-refund-v1','test-retention');")
 policy={'versionLabel':'test-policy','periodMonths':1,'computeMonthlyUSDMicros':'0','storageMonthlyUSDMicros':'0','productMonthlyUSDMicros':'0','validFrom':'2026-01-01T00:00:00Z','computePlanId':'test-compute','storagePlanId':'test-storage','planChangePolicyVersion':'workspace-plan-change-v1','renewalPolicy':{'version':'renewal-policy/v1','trigger':'manual_or_explicitly_consented_automatic','effectiveStart':'previous_paid_through','months':1,'usesAcceptedPriceSnapshot':True}}
 api_validate('CreatePricePolicyRequest',policy)
 db.insert('resource_catalog','price_policy_versions',id='test-price',version_label=policy['versionLabel'],compute_plan_id=policy['computePlanId'],storage_plan_id=policy['storagePlanId'],compute_monthly_usd_micros=money(policy['computeMonthlyUSDMicros']),storage_monthly_usd_micros=money(policy['storageMonthlyUSDMicros']),product_monthly_usd_micros=money(policy['productMonthlyUSDMicros']),currency='USD',plan_change_policy_version=policy['planChangePolicyVersion'],renewal_rules=policy['renewalPolicy'],valid_from=policy['validFrom'],published_by='test-admin',period_months=policy['periodMonths'])


def credit_source(db,case,amount):
 sid=case['id'];workspace='source-ws-'+sid;seed_workspace(db,workspace)
 db.insert('gateway','tenant_wallet_bindings',id='binding-'+sid,tenant_id='tenant-'+sid,billing_sub2api_user_id='subject-'+sid,delegation_ref='delegation-test',verification_evidence_ref='delegation-readback-test',bound_at=NOW)
 confirmed=case.get('creditSourceConfirmed',True)
 db.insert('gateway','wallet_operations',id='charge-'+sid,tenant_id='tenant-'+sid,wallet_binding_id='binding-'+sid,workspace_id=workspace,kind='charge',status='confirmed' if confirmed else 'unknown',amount_usd_micros=100,business_idempotency_key='original-charge-key-'+sid,request_fingerprint='original-request-hash',external_reference='external-original-'+sid if confirmed else None,confirmed_at=NOW if confirmed else None)
 db.insert('workspace','subscriptions',id='subscription-'+sid,workspace_id=workspace,billing_subject_ref='binding-'+sid,current_period_start='2026-09-01T00:00:00Z',current_period_end='2026-10-01T00:00:00Z',period_months=1,provenance='legacy_import',legacy_purchase_id='original-purchase-'+sid,legacy_obligation_snapshot={'originalChargeId':'charge-'+sid},renewal_mode='manual')
 db.insert('workspace','subscription_periods',id='period-'+sid,subscription_id='subscription-'+sid,period_start='2026-09-01T00:00:00Z',period_end='2026-10-01T00:00:00Z',billing_key='original-period-key-'+sid,charge_wallet_operation_id='charge-'+sid,charge_receipt_id='charge-receipt-'+sid,provenance='legacy_import',legacy_purchase_id='original-purchase-'+sid,legacy_obligation_snapshot={'originalChargeId':'charge-'+sid})
 evidence={'outcome':'confirmed' if confirmed else 'unknown','originalWalletOperationId':'charge-'+sid,'originalSubscriptionPeriodId':'period-'+sid,'policyVersionId':'approved-original-policy-test','amountUSDMicros':case.get('creditSourceAmountUSDMicros',amount)}
 eb=json.dumps(evidence,sort_keys=True,separators=(',',':')).encode()
 db.insert('ledger','receipts',id='credit-receipt-'+sid,source_owner='gateway',source_event_id='credit-event-'+sid,kind='confirmed_credit_fixture',subject_type='workspace',subject_id=workspace,source_operation_id='old-credit-operation-'+sid,request_id='old-credit-request-'+sid,evidence_sha256=hashlib.sha256(eb).hexdigest(),evidence=evidence,source_occurred_at=NOW)
 return {'originalWalletOperationId':'charge-'+sid,'originalSubscriptionPeriodId':'period-'+sid,'policyVersionId':'approved-original-policy-test','creditReceiptId':'credit-receipt-'+sid,'amountUSDMicros':case.get('creditSourceAmountUSDMicros',amount)}

def verify_credit_readback(db,source,line_amount):
 # Separate owner connections are fixture readback adapters, not product cross-database JOINs.
 charge=db.one_json('gateway','SELECT status FROM gateway.wallet_operations WHERE id='+q(source['originalWalletOperationId']))
 period=db.one_json('workspace','SELECT charge_wallet_operation_id FROM workspace.subscription_periods WHERE id='+q(source['originalSubscriptionPeriodId']))
 receipt=db.one_json('ledger','SELECT evidence FROM ledger.receipts WHERE id='+q(source['creditReceiptId']))
 owner_assert(charge and charge['status']=='confirmed','original Gateway charge is not confirmed')
 owner_assert(period and period['charge_wallet_operation_id']==source['originalWalletOperationId'],'credit original period/charge mismatch')
 owner_assert(receipt and receipt['evidence']['outcome']=='confirmed','credit receipt not confirmed')
 owner_assert(all(receipt['evidence'].get(k)==source[k] for k in ['originalWalletOperationId','originalSubscriptionPeriodId','policyVersionId','amountUSDMicros']),'credit provenance mismatch')
 owner_assert(money(source['amountUSDMicros'])==line_amount,'credit source amount differs from line')

def quote_case(db,pb,case):
 request={'purpose':case['purpose'],'capabilityVersionId':'test-capability','computePlanId':'test-compute','storagePlanId':'test-storage','modelSelections':[],'periodMonths':1}
 api_validate('QuoteRequest',request)
 lines=[]
 for x in case['lines']:
  line={'kind':x['kind'],'description':'isolated semantic fixture','quantity':1,'amountUSDMicros':x['amountUSDMicros']}
  if line['kind']=='adjustment_credit':line['creditSource']=credit_source(db,case,line['amountUSDMicros'])
  api_validate('QuoteLine',line)
  n=money(line['amountUSDMicros'])
  if line['kind']=='adjustment_credit':verify_credit_readback(db,line['creditSource'],n)
  lines.append(line)
 total=sum(money(x['amountUSDMicros'])*(-1 if x['kind']=='adjustment_credit' else 1) for x in lines)
 owner_assert(0<=total<=MAX_INT64,'total out of nonnegative int64 range')
 owner_assert('claimedTotalUSDMicros' not in case or money(case['claimedTotalUSDMicros'])==total,'claimed total uses different sign convention')
 sid=case['id']
 db.insert('resource_catalog','quotes',id=sid,tenant_id='test-tenant',actor_id='test-actor',purpose=request['purpose'],capability_version_id=request['capabilityVersionId'],compute_plan_id=request['computePlanId'],storage_plan_id=request['storagePlanId'],price_policy_version_id='test-price',refund_policy_version_id='test-refund',retention_policy_version_id='test-retention',total_usd_micros=total,period_start='2026-09-21T12:00:00Z',period_end='2026-10-21T12:00:00Z',input_digest=digest(json.dumps(request,sort_keys=True).encode()),admission_snapshot={'scope':'isolated-test'},expires_at='2026-09-21T13:00:00Z',model_selections=[],period_months=1,refund_terms='test-only',retention_terms='test-only',expected_interruption='none')
 for n,line in enumerate(lines):db.insert('resource_catalog','quote_items',id=sid+'-line-'+str(n),quote_id=sid,kind=line['kind'],description=line['description'],amount_usd_micros=money(line['amountUSDMicros']),calculation={'source':'typed-api-line-total'},sort_order=n,quantity=line['quantity'],credit_source=line.get('creditSource'))
 actual=db.one_json('resource_catalog','SELECT purpose,total_usd_micros::text AS total FROM resource_catalog.quotes WHERE id='+q(sid))
 signed=db.sql('resource_catalog',"SELECT sum(CASE WHEN kind='adjustment_credit' THEN -amount_usd_micros ELSE amount_usd_micros END)::text FROM resource_catalog.quote_items WHERE quote_id="+q(sid)+';')
 assert actual=={'purpose':request['purpose'],'total':str(total)} and signed==str(total)
 assert str(total)==case['expectedTotalUSDMicros']
 for line in lines:api_validate('QuoteLine',line)
 return {'input':request,'persistedPurpose':actual['purpose'],'persistedAndProjectedTotalUSDMicros':actual['total'],'databaseSignedSumUSDMicros':signed,'allLineAmountsNonnegative':True}


STORAGE={}
def publisher_decode(db,request,expected_publisher_kind='official'):
 api_validate('RegisterRuntimeVersionRequest',request)
 descriptor=request['publisherContract'];ns=db.one_json('capability','SELECT kind,repository_prefix,status FROM capability.publisher_namespaces WHERE id='+q(request['publisherNamespaceId']))
 owner_assert(ns is not None and ns['status']=='approved','publisher namespace not admitted')
 owner_assert(ns['kind']==expected_publisher_kind,'official and third-party publisher namespaces are not interchangeable')
 owner_assert(request['publisherNamespaceId']==descriptor['publisherNamespaceId'],'descriptor namespace differs from request')
 repository=descriptor['image']['repository'];prefix=ns['repository_prefix']
 owner_assert(repository==prefix or repository.startswith(prefix+'/'),'repository is not within exact path-segment namespace')
 image=descriptor['image'];template=descriptor['applicationRevisionTemplate'];platform=image['platform'];platform_string=platform['os']+'/'+platform['architecture']+('/'+platform['variant'] if 'variant' in platform else '')
 owner_assert(template['image']==image['repository']+'@'+image['digest'],'canonical application template image differs from ArtifactReference')
 owner_assert(template['platform']==platform_string,'canonical application template platform differs from ArtifactReference')
 encoded=json.dumps(descriptor,ensure_ascii=False,separators=(',',':')).encode('utf-8')
 object_ref='fixture-storage:'+hashlib.sha256(encoded).hexdigest();STORAGE[object_ref]=encoded
 return descriptor,digest(encoded),object_ref

def insert_runtime(db,ident,request,expected_kind='official'):
 desc,sha,objref=publisher_decode(db,request,expected_kind)
 db.sql('runtime_control',"INSERT INTO runtime_control.runtime_releases(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_version,package_format_versions,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES ("+','.join([q(ident),q(request['name']),q(request['versionLabel']),q(desc['image']['repository']),q(desc['image']['digest']),q('test-admin'),q(desc['runtimeAbiVersion']),"ARRAY["+','.join(q(x) for x in desc['packageFormatVersions'])+"]::text[]",q(request['admissionReceiptId']),q(desc['publisherNamespaceId']),q(sha),j(desc),q(objref)])+");")
 return sha,objref

def seed_publishers(db):
 for ident,kind,prefix in [('pub_official','official','registry.example/official'),('pub_acme','third_party','registry.example/third-party/acme')]:
  db.insert('capability','publisher_namespaces',id=ident,name=ident,kind=kind,registry_id='test-registry',repository_prefix=prefix,admission_receipt_id='namespace-admission-'+ident)
 desc=copy.deepcopy(PUBLISHER['examples'][0]);insert_runtime(db,'runtime-base',{'name':'base-runtime','versionLabel':'v1','publisherNamespaceId':desc['publisherNamespaceId'],'publisherContract':desc,'admissionReceiptId':'runtime-admission-test'})
 desc=copy.deepcopy(PUBLISHER['examples'][1]);request={'name':'base-ui','versionLabel':'v1','publisherNamespaceId':desc['publisherNamespaceId'],'publisherContract':desc,'admissionReceiptId':'ui-admission-test'}
 api_validate('RegisterWebuiVersionRequest',request)
 encoded=json.dumps(desc,separators=(',',':')).encode();sha=digest(encoded);objref='fixture-storage:'+sha;STORAGE[objref]=encoded
 db.sql('capability',"INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES ('webui-base','base-ui','v1',"+q(desc['image']['repository'])+','+q(desc['image']['digest'])+",'test-admin',ARRAY["+','.join(q(x) for x in desc['runtimeAbiVersions'])+']::text[],'+q(desc['uiProtocolVersion'])+",'ui-admission-test','pub_official',"+q(sha)+','+j(desc)+','+q(objref)+');')
 db.insert('capability','namespaces',id='agent-namespace',tenant_id='test-tenant',name='agent-tests',kind='tenant_default')
 db.insert('capability','packages',id='package-base',namespace_id='agent-namespace',name='package-test',visibility='private',created_by='test-actor')
 db.insert('capability','package_versions',id='package-version-base',package_id='package-base',version_label='v1',status='uploaded',sha256='sha256:'+'1'*64,size_bytes=100,object_ref='fixture-package-object',manifest={'formatVersion':'oma-package/v1'},created_by='test-actor',verified_at=NOW)
 runtime=copy.deepcopy(PUBLISHER['examples'][0]);webui=copy.deepcopy(PUBLISHER['examples'][1]);artifact={'repository':'registry.example/private/result','digest':'sha256:'+'2'*64,'platform':runtime['image']['platform']};revision=copy.deepcopy(runtime['applicationRevisionTemplate']);revision['image']=artifact['repository']+'@'+artifact['digest']
 refs={}
 for name,ident in [('runtime','runtime-base'),('webui','webui-base')]:
  rr=db.one_json('runtime_control' if name=='runtime' else 'capability','SELECT publisher_namespace_id,publisher_contract_digest,publisher_contract_object_ref FROM '+('runtime_control.runtime_releases' if name=='runtime' else 'capability.webui_versions')+' WHERE id='+q(ident))
  refs[name]={'publisherNamespaceId':rr['publisher_namespace_id'],'versionId':ident,'kind':name,'descriptorDigest':rr['publisher_contract_digest'],'descriptorObjectRef':rr['publisher_contract_object_ref']}
 descriptor={'schemaVersion':'opl-deployment-descriptor/v1','artifact':artifact,'runtimeContract':runtime,'runtimeContractReference':refs['runtime'],'webuiContract':webui,'webuiContractReference':refs['webui'],'provenance':'build','applicationRevision':revision,'packageVersionId':'package-version-base','buildInputDigest':'sha256:'+'3'*64}
 api_validate('DeploymentDescriptor',descriptor);raw=json.dumps(descriptor,separators=(',',':')).encode();descriptor_sha=digest(raw);descriptor_ref='fixture-deployment:'+descriptor_sha;STORAGE[descriptor_ref]=raw
 db.insert('capability','capability_versions',deployment_descriptor=descriptor,deployment_descriptor_digest=descriptor_sha,deployment_descriptor_object_ref=descriptor_ref,id='capability-base',package_id='package-base',package_version_id='package-version-base',build_job_id='build-base',version_label='v1',runtime_version_id='runtime-base',webui_version_id='webui-base',artifact_repository='registry.example/private/result',artifact_digest='sha256:'+'2'*64,model_requirements=[],data_compatibility={'dataSchemaVersion':'v1'},provenance_evidence={'scope':'isolated-fixture'},provenance='build')

def publisher_case(db,pb,case):
 desc=copy.deepcopy(PUBLISHER['examples'][0 if case['publisherKind']=='official' else 2]);mutation=case.get('mutation')
 if mutation=='missing_template':del desc['applicationRevisionTemplate']
 elif mutation=='template_image_mismatch':desc['applicationRevisionTemplate']['image']='registry.example/other/image@'+'sha256:'+'9'*64
 elif mutation=='prefix_confusion':
  desc['image']['repository']='registry.example/third-party/acme-evil/runtime';desc['applicationRevisionTemplate']['image']=desc['image']['repository']+'@'+desc['image']['digest']
 elif mutation=='official_namespace':desc['publisherNamespaceId']='pub_official'
 request={'name':case['id'],'versionLabel':'v1','publisherNamespaceId':desc['publisherNamespaceId'],'publisherContract':desc,'admissionReceiptId':'admission-'+case['id']}
 sha,objref=insert_runtime(db,case['id'],request,case['publisherKind'])
 if mutation=='stored_digest_mismatch':STORAGE[objref]=b'altered immutable descriptor bytes'
 row=db.one_json('runtime_control','SELECT publisher_contract,publisher_contract_digest,publisher_contract_object_ref,artifact_repository,artifact_digest FROM runtime_control.runtime_releases WHERE id='+q(case['id']))
 exact_bytes=STORAGE[row['publisher_contract_object_ref']]
 owner_assert(digest(exact_bytes)==row['publisher_contract_digest'],'immutable descriptor object digest readback mismatch')
 api_validate('RuntimePublisherContract',json.loads(exact_bytes))
 assert row['publisher_contract']==desc and row['artifact_repository']==desc['image']['repository'] and row['artifact_digest']==desc['image']['digest']
 return {'publisherKind':case['publisherKind'],'descriptorDigest':sha,'descriptorStorage':'isolated in-memory immutable Storage fixture','persistedRepository':row['artifact_repository'],'roundtripContractEqual':True}


def reference_case(db,pb,case):
 mapping={'packageVersionId':('package_version','package_version_id','package-version-base'),'runtimeVersionId':('runtime_version','runtime_version_id','runtime-base'),'webuiVersionId':('webui_version','webui_version_id','webui-base'),'capabilityVersionId':('capability_version','capability_version_id','capability-base')}
 target={}
 if case.get('targetField'):target[case['targetField']]=mapping[case['targetField']][2]
 if case.get('extraTarget'):target[case['extraTarget']]=mapping[case['extraTarget']][2]
 parsed=proto_validate(pb,'ReferenceTarget',target)
 if len(parsed.ListFields())!=1:raise Rejection('decoder_reject','ReferenceTarget requires exactly one nonempty target')
 typ,column,identity=mapping[case['targetField']];sid=case['id'];input_digest='sha256:'+'3'*64
 db.insert('capability','reference_claims',id=sid,target_type=typ,**{column:identity},claimant_owner='build',claimant_resource_id='job-'+sid,purpose='build_input',request_id='request-'+sid)
 db.insert('build','operations',id='operation-'+sid,tenant_id='test-tenant',actor_id='test-actor',kind='create_build',resource_id='job-'+sid,status='running',stage='validating',request_id='request-'+sid,accepted_input={'inputDigest':input_digest},result={'committedVersion':1},created_at=NOW)
 db.insert('build','build_jobs',id='job-'+sid,tenant_id='test-tenant',package_version_id='package-version-base',runtime_version_id='runtime-base',webui_version_id='webui-base',input_digest=input_digest,input_snapshot={'claimIds':[sid],'committedVersion':1},status='validating',stage='validating',request_id='request-'+sid,created_by='test-actor',operation_id='operation-'+sid,catalog_policy_id='policy-test')
 commit={'owner':'OWNER_ENUM_BUILD','operationId':'operation-'+sid,'resourceId':'job-'+sid,'acceptedInputDigest':input_digest,'committedVersion':'1','acceptedAt':NOW}
 bind={'claimId':sid,'ownerCommitEvidence':commit}
 proto_validate(pb,'BindReferenceRequest',bind)
 committed=db.one_json('build','SELECT b.id,b.operation_id,b.input_digest,b.input_snapshot,o.accepted_input,o.created_at FROM build.build_jobs b JOIN build.operations o ON o.id=b.operation_id WHERE b.id='+q('job-'+sid))
 owner_assert(committed and committed['operation_id']==commit['operationId'] and committed['input_digest']==commit['acceptedInputDigest'] and sid in committed['input_snapshot']['claimIds'],'Build owner committed input/claim identity must match Bind evidence')
 db.sql('capability','UPDATE capability.reference_claims SET bound_operation_id='+q(commit['operationId'])+',bound_input_digest='+q(input_digest)+',bound_at='+q(NOW)+',updated_at='+q(NOW)+' WHERE id='+q(sid)+' AND bound_at IS NULL;')
 if case.get('mutation')=='bind_digest_changed':
  existing=db.one_json('capability','SELECT bound_operation_id,bound_input_digest FROM capability.reference_claims WHERE id='+q(sid))
  owner_assert(existing['bound_input_digest']=='sha256:'+'4'*64,'Bind same claim/operation different input digest is IDEMPOTENCY_CONFLICT')
 evidence={'owner':'OWNER_ENUM_BUILD','operationId':'operation-'+sid,'resourceId':'job-'+sid,'terminalStatus':'TERMINAL_OPERATION_STATUS_FAILED','terminalReceiptId':'receipt-'+sid}
 if case.get('mutation')=='missing_release_receipt':del evidence['terminalReceiptId']
 # Proto supplies shape; owner readback supplies required terminal/receipt truth.
 proto_validate(pb,'ReleaseEvidence',evidence)
 owner_assert(bool(evidence.get('terminalReceiptId')),'release requires original owner receipt')
 terminal_status='needs_attention' if case.get('mutation')=='unknown_release' else 'failed'
 observation='unknown' if case.get('mutation')=='unknown_release' else 'confirmed'
 db.sql('build','UPDATE build.build_jobs SET status='+q(terminal_status)+',stage='+q(terminal_status)+',finished_at='+q(NOW)+' WHERE id='+q('job-'+sid)+'; UPDATE build.operations SET status='+q(terminal_status)+',stage='+q(terminal_status)+',observation_result='+q(observation)+',result='+j({'terminalReceiptId':'receipt-'+sid})+' WHERE id='+q('operation-'+sid)+';')
 usage=db.one_json('build','SELECT b.status,b.operation_id,b.input_digest,o.observation_result,o.result FROM build.build_jobs b JOIN build.operations o ON o.id=b.operation_id WHERE b.id='+q('job-'+sid))
 owner_assert(evidence['terminalStatus']=='TERMINAL_OPERATION_STATUS_FAILED' and usage['status']=='failed' and usage['observation_result']=='confirmed','Build owner readback remains active/unknown: cannot release input protection')
 owner_assert(usage['operation_id']==evidence['operationId'] and usage['result']['terminalReceiptId']==evidence['terminalReceiptId'],'self-reported release receipt does not match original owner terminal record')
 db.sql('capability','UPDATE capability.reference_claims SET release_evidence_ref='+q(evidence['terminalReceiptId'])+',release_evidence='+j(evidence)+',released_at='+q(NOW)+' WHERE id='+q(sid)+';')
 row=db.one_json('capability','SELECT target_type,'+column+' AS target_id,bound_operation_id,bound_input_digest,release_evidence FROM capability.reference_claims WHERE id='+q(sid))
 assert row['target_type']==typ and row['target_id']==identity and row['release_evidence']==evidence
 return {'decodedTarget':target,'persistedTargetType':row['target_type'],'boundInputDigest':row['bound_input_digest'],'releaseEvidenceRoundtrip':True}

def authorization_case(db,pb,case):
 sid=case['id'];tenant='tenant-'+sid;actor='actor-'+sid;session='session-'+sid;context='context-'+sid;workspace='ws-'+sid
 db.insert('tenant','tenants',id=tenant,name=tenant,permission_version=7)
 db.insert('tenant','tenant_members',id='member-'+sid,tenant_id=tenant,actor_id=actor,role='admin')
 db.insert('tenant','sessions',id=session,session_hash='hash-'+sid,actor_id=actor,tenant_id=tenant,gateway_session_ref='gateway-session-reference',csrf_hash='csrf-hash',expires_at='2026-09-22T12:00:00Z')
 db.insert('workspace','workspaces',id=workspace,tenant_id=tenant,name=workspace,status='active',capability_version_id='capability-base',compute_plan_id='test-compute',storage_plan_id='test-storage',created_by=actor,delivery_model='agent_saas',version=1)
 decision={'result':'AUTHORIZATION_RESULT_ALLOWED','issuer':'AUTHORIZATION_ISSUER_CLOUD_IDENTITY','authorizationContextId':context,'scope':{'tenant':{'tenantId':tenant}},'actorId':actor,'sessionId':session,'audienceOwner':'OWNER_ENUM_WORKSPACE','action':'AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION','resource':{'kind':'AUTHORIZATION_RESOURCE_KIND_WORKSPACE','id':workspace},'permissionVersion':'7','issuedAt':NOW,'expiresAt':'2026-09-21T12:05:00Z'}
 proto_validate(pb,'AuthorizationDecision',decision)
 db.insert('tenant','authorization_contexts',id=context,scope_type='tenant',tenant_id=tenant,actor_id=actor,session_id=session,permission_version=7,audience_owner='workspace',action='updateWorkspaceVersion',resource_kind='workspace',resource_id=workspace,issuer='cloud_identity',issued_at=NOW,expires_at='2026-09-21T12:05:00Z')
 request={'scope':{'tenant':{'tenantId':tenant}},'actorId':actor,'sessionId':session,'authorizationContextId':context,'audienceOwner':'OWNER_ENUM_WORKSPACE','action':'AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION','resource':{'kind':'AUTHORIZATION_RESOURCE_KIND_WORKSPACE','id':workspace},'expectedPermissionVersion':'7','requestId':'request-'+sid}
 caller='workspace';mutation=case.get('mutation')
 if mutation=='tenant':request['scope']['tenant']['tenantId']='another-tenant'
 elif mutation=='audience':request['audienceOwner']='OWNER_ENUM_FABRIC'
 elif mutation=='action':request['action']='AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE'
 elif mutation=='resource':request['resource']['id']='another-workspace'
 elif mutation=='permission_version':db.sql('tenant','UPDATE tenant.tenants SET permission_version=8 WHERE id='+q(tenant)+';')
 elif mutation=='session_revoked':db.sql('tenant','UPDATE tenant.sessions SET revoked_at='+q(NOW)+' WHERE id='+q(session)+';')
 elif mutation=='mtls_caller':caller='fabric'
 proto_validate(pb,'AuthorizationRequest',request)
 row=db.one_json('tenant','SELECT c.*,t.status AS tenant_status,t.permission_version AS current_permission_version,s.revoked_at AS session_revoked,m.revoked_at AS membership_revoked,m.role FROM tenant.authorization_contexts c JOIN tenant.tenants t ON t.id=c.tenant_id JOIN tenant.sessions s ON s.id=c.session_id JOIN tenant.tenant_members m ON m.tenant_id=c.tenant_id AND m.actor_id=c.actor_id WHERE c.id='+q(context))
 owner_assert(caller=='workspace','authenticated mTLS caller is not permitted for this audience/action; body cannot override it')
 owner_assert(row['tenant_id']==request['scope']['tenant']['tenantId'],'authorization scope tenant mismatch')
 owner_assert(request['audienceOwner']=='OWNER_ENUM_WORKSPACE','authorization audience mismatch')
 owner_assert(request['action']=='AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION','authorization action mismatch')
 owner_assert(row['resource_id']==request['resource']['id'],'authorization resource mismatch')
 owner_assert(row['tenant_status']=='active' and row['current_permission_version']==row['permission_version'],'stale/revoked tenant permission version')
 owner_assert(row['session_revoked'] is None and row['membership_revoked'] is None and row['role'] in ['admin','owner'],'session/member access revoked')
 resource=db.one_json('workspace','SELECT tenant_id FROM workspace.workspaces WHERE id='+q(request['resource']['id']))
 owner_assert(resource and resource['tenant_id']==row['tenant_id'],'workspace owner readback rejects cross-Tenant resource')
 return {'proto':'AuthorizationRequest','persistedScope':row['scope_type'],'audience':row['audience_owner'],'ownerResourceScopeVerified':True,'mtlsIdentityFromBody':False}

def authorization_platform_case(db,pb,case):
 sid=case['id'];session='session-'+sid
 db.insert('tenant','sessions',id=session,session_hash='hash-'+sid,actor_id='platform-actor',tenant_id=None,gateway_session_ref='gateway-platform-session',csrf_hash='csrf',expires_at='2026-09-22T12:00:00Z')
 scope={'platform':{}};proto_validate(pb,'AuthorizationScope',scope)
 db.insert('tenant','authorization_contexts',id=sid,scope_type='platform',tenant_id=None,actor_id='platform-actor',session_id=session,permission_version=1,audience_owner='capability',action='registerRuntimeVersion',resource_kind='catalog',issuer='cloud_identity',issued_at=NOW,expires_at='2026-09-21T12:05:00Z')
 row=db.one_json('tenant','SELECT scope_type,tenant_id FROM tenant.authorization_contexts WHERE id='+q(sid));assert row=={'scope_type':'platform','tenant_id':None}
 return {'scope':scope,'persistedTenantId':None,'fixturePlatformRoleReadback':'explicit authorized platform actor; no Tenant-derived privilege'}

def authorization_grant_case(db,pb,case):
 sid=case['id'];tenant='tenant-'+sid;db.insert('tenant','tenants',id=tenant,name=tenant,status='suspended',permission_version=8)
 allowed=['completeAcceptedObligation','cancelAcceptedObligation','closeoutAcceptedObligation'];mode='closeout_only'
 grant={'id':sid,'scope':{'tenant':{'tenantId':tenant}},'actorId':'old-actor','acceptedOperationOwner':'OWNER_ENUM_WORKSPACE','acceptedOperationId':'original-operation-'+sid,'acceptedAction':'AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION','resourceId':'original-workspace','acceptedPermissionVersion':'7','allowedActions':['AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION','AUTHORIZATION_ACTION_ENUM_CANCELACCEPTEDOBLIGATION','AUTHORIZATION_ACTION_ENUM_CLOSEOUTACCEPTEDOBLIGATION'],'mode':'ACCEPTED_GRANT_MODE_CLOSEOUT_ONLY','issuedAt':NOW}
 proto_validate(pb,'AcceptedOperationGrant',grant)
 db.sql('tenant',"INSERT INTO tenant.accepted_operation_grants(id,scope_type,tenant_id,actor_id,accepted_operation_owner,accepted_operation_id,accepted_action,resource_id,accepted_permission_version,allowed_actions,mode,issued_at) VALUES ("+','.join([q(sid),q('tenant'),q(tenant),q('old-actor'),q('workspace'),q('original-operation-'+sid),q('updateWorkspaceVersion'),q('original-workspace'),'7',"ARRAY["+','.join(q(x) for x in allowed)+"]::text[]",q(mode),q(NOW)])+');')
 action='purchaseResources' if case.get('mutation')=='new_purchase' else 'closeoutAcceptedObligation'
 row=db.one_json('tenant','SELECT allowed_actions,mode,accepted_operation_id,resource_id FROM tenant.accepted_operation_grants WHERE id='+q(sid))
 owner_assert(action in row['allowed_actions'] and row['mode']=='closeout_only','revoked member/Tenant cannot expand an accepted grant into a new purchase')
 return {'mode':mode,'permittedOriginalAction':action,'noNewProcurementAuthority':True}


def seed_route_resources(db,sid):
 ws='ws-'+sid;seed_workspace(db,ws,epoch=2)
 db.insert('workspace','operations',id='op-'+sid,tenant_id='test-tenant',actor_id='test-actor',kind='update_workspace',resource_id=ws,stage='runtime_start',request_id='req-'+sid,accepted_input={'scope':'test'})
 db.insert('serve','agent_deployments',id='dep-'+sid,workspace_id=ws,capability_version_id='capability-base',artifact_digest='sha256:'+'2'*64,reference_claim_id='claim-'+sid,operation_id='serve-op-'+sid,status='verifying',data_compatibility={'dataSchemaVersion':'v1'},execution_epoch=2)
 db.insert('fabric','resource_sets',id='set-'+sid,tenant_id='test-tenant',workspace_id=ws,provider='local',provider_profile_ref='isolated',region='local',compute_plan_id='test-compute',storage_plan_id='test-storage',accepted_quote_id='quote-test',approved_specification={'scope':'test'},observation_result='confirmed',observed_at=NOW)
 for label in ['old','new']:
  db.insert('fabric','resources',id=label+'-'+sid,resource_set_id='set-'+sid,kind='execution',provider_resource_ref=label+'-provider-'+sid,provider_purchase_key=label+'-key-'+sid,billing_mode='LOCAL_NO_CHARGE',requested_specification={'scope':'test'},observed_specification={'scope':'test'},observation_result='confirmed',observed_at=NOW)
 db.insert('serve','access_bindings',id='route-'+sid,workspace_id=ws,route_generation=0,accepted_execution_epoch=1,target_execution_resource_id='old-'+sid,provider_revision='r0',observed_at=NOW)
 return ws

class ProviderRouteFixture:
 """Explicit provider boundary simulator; it does not claim a real adapter is qualified."""
 def __init__(self,target):self.revision='r0';self.epoch=1;self.target=target;self.sequence=0
 def cas(self,expected_revision,epoch,target):
  if self.revision!=expected_revision:raise Rejection('provider_reject','provider conditional revision rejects late route mutation')
  if epoch<self.epoch:raise Rejection('provider_reject','provider epoch metadata rejects stale execution')
  self.sequence+=1;self.revision='r'+str(self.sequence);self.epoch=epoch;self.target=target
  return {'revision':self.revision,'epoch':self.epoch,'target':self.target}

def route_case(db,pb,case):
 sid=case['id'];ws=seed_route_resources(db,sid);binding='route-'+sid;provider=ProviderRouteFixture('old-'+sid)
 context={'requestId':'req-'+sid,'idempotencyKey':'key-'+sid,'authorizationContextId':'fixture-context','actorId':'test-actor','scope':{'tenant':{'tenantId':'test-tenant'}},'deadlineAt':'2026-09-21T12:05:00Z'}
 fence={'context':context,'workspaceId':ws,'operationId':'serve-op-'+sid,'executionEpoch':'2','expectedRouteGeneration':'0','providerPrecondition':{'exactRevision':'r0'}}
 proto_validate(pb,'FenceRouteEpochCommand',fence)
 db.insert('serve','access_switches',id='fence-'+sid,route_binding_id=binding,workspace_id=ws,operation_owner='serve',operation_id='serve-op-'+sid,expected_route_generation=0,execution_epoch=2,target_execution_resource_id='old-'+sid,provider_command_id='provider-fence-'+sid,action_kind='fence',expected_provider_revision='r0')
 observed=provider.cas('r0',2,'old-'+sid)
 db.sql('serve',"UPDATE serve.access_switches SET status='confirmed',observed_route_generation=0,observed_execution_epoch=2,observed_provider_revision='r1',evidence_ref='fence-receipt-test' WHERE id="+q('fence-'+sid)+"; UPDATE serve.access_bindings SET accepted_execution_epoch=2,provider_revision='r1',last_confirmed_switch_id="+q('fence-'+sid)+' WHERE id='+q(binding)+" AND accepted_execution_epoch=1 AND route_generation=0 AND provider_revision='r0';")
 mutation=case.get('mutation');epoch=1 if mutation=='epoch' else 2;generation=1 if mutation=='generation' else 0
 activation={'context':context,'workspaceId':ws,'operationId':'serve-op-'+sid,'executionEpoch':str(epoch),'expectedRouteGeneration':str(generation),'providerPrecondition':{'exactRevision':'r1'},'targetExecutionResourceId':'new-'+sid,'targetRuntimeInstanceId':'runtime-'+sid,'targetDeploymentId':'dep-'+sid,'confirmedReadinessReceiptId':'readiness-'+sid}
 proto_validate(pb,'RouteActivateCommand',activation)
 current=db.one_json('serve','SELECT route_generation,accepted_execution_epoch,provider_revision FROM serve.access_bindings WHERE id='+q(binding))
 owner_assert(current['route_generation']==generation,'stale route generation')
 owner_assert(current['accepted_execution_epoch']==epoch,'stale execution epoch despite route generation match')
 if mutation=='late_provider':provider.cas('r0',1,'old-'+sid)
 if mutation=='unknown_pending':
  db.insert('serve','access_switches',id='unknown-'+sid,route_binding_id=binding,workspace_id=ws,operation_owner='serve',operation_id='serve-op-'+sid,expected_route_generation=0,execution_epoch=2,target_execution_resource_id='new-'+sid,provider_command_id='unknown-provider-command-'+sid,status='unknown',action_kind='activate',expected_provider_revision='r1')
 try:
  # Atomic local reserve includes exact generation/epoch/revision predicate. An empty
  # RETURNING means reject before provider mutation, not a successful no-op.
  sql="WITH locked AS (SELECT id FROM serve.access_bindings WHERE id="+q(binding)+" AND route_generation=0 AND accepted_execution_epoch=2 AND provider_revision='r1' FOR UPDATE) INSERT INTO serve.access_switches(id,route_binding_id,workspace_id,operation_owner,operation_id,expected_route_generation,execution_epoch,target_execution_resource_id,previous_target_execution_resource_id,provider_command_id,action_kind,expected_provider_revision) SELECT "+','.join([q('activate-'+sid),'id',q(ws),q('serve'),q('serve-op-'+sid),'0','2',q('new-'+sid),q('old-'+sid),q('activate-command-'+sid),q('activate'),q('r1')])+" FROM locked RETURNING id;"
  owner_assert(bool(db.sql('serve',sql)),'local CAS reservation rejected')
 except PgError as e:
  if e.state=='23505':raise Rejection('database_reject','one pending/unknown route action per Workspace')
  raise
 observed=provider.cas('r1',2,'new-'+sid)
 db.sql('serve',"UPDATE serve.access_switches SET status='confirmed',observed_route_generation=1,observed_execution_epoch=2,observed_provider_revision='r2',evidence_ref='route-readback-test' WHERE id="+q('activate-'+sid)+"; UPDATE serve.access_bindings SET route_generation=1,target_execution_resource_id="+q('new-'+sid)+",provider_revision='r2',last_confirmed_switch_id="+q('activate-'+sid)+' WHERE id='+q(binding)+" AND route_generation=0 AND accepted_execution_epoch=2 AND provider_revision='r1';")
 if mutation=='commit_epoch':db.sql('serve','UPDATE serve.agent_deployments SET execution_epoch=3 WHERE id='+q('dep-'+sid)+';')
 selected=db.sql('serve','UPDATE serve.agent_deployments SET status=\'active\',runtime_instance_id='+q('runtime-'+sid)+',verification_evidence_ref='+q('readiness-'+sid)+',activated_at='+q(NOW)+',selection_commit_receipt_id='+q('selection-commit-'+sid)+' WHERE id='+q('dep-'+sid)+" AND status='verifying' AND execution_epoch=2 RETURNING id;")
 owner_assert(bool(selected),'Serve selection commit stale intent/epoch; do not claim routed target is selected')
 db.sql('serve','UPDATE serve.access_switches SET selection_commit_receipt_id='+q('selection-commit-'+sid)+' WHERE id='+q('activate-'+sid)+';')
 row=db.one_json('serve','SELECT id,status,execution_epoch,selection_commit_receipt_id FROM serve.agent_deployments WHERE id='+q('dep-'+sid));assert row=={'id':'dep-'+sid,'status':'active','execution_epoch':2,'selection_commit_receipt_id':'selection-commit-'+sid}
 return {'fenceConfirmedBeforeActivate':True,'providerFixture':observed,'selectedCommit':row,'externalRouterAtomicWithDatabase':False}


def tenant_lifecycle_case(db,pb,case):
 sid=case['id'];mutation=case.get('mutation');action=case['action'];state='suspended' if action=='reenable' and mutation!='deleted' else 'deleted'
 deleted='2026-09-01T12:00:00Z' if mutation=='expired' else '2026-09-15T12:00:00Z';deadline='2026-09-16T12:00:00Z' if mutation=='expired' else '2026-09-30T12:00:00Z'
 data={'id':sid,'name':sid,'status':state,'permission_version':4}
 if state=='suspended':data['suspended_at']='2026-09-20T12:00:00Z'
 else:data.update(deletion_requested_at='2026-09-01T12:00:00Z' if mutation=='request_earlier' else deleted,deleted_at=deleted,restore_until=deadline)
 db.insert('tenant','tenants',**data)
 api_validate('TenantActionRequest',{'reason':'isolated lifecycle semantic verification'})
 before=db.sql('fabric','SELECT count(*) FROM fabric.resources;')
 if action=='reenable':
  row=db.sql('tenant',"UPDATE tenant.tenants SET status='active',suspended_at=NULL,permission_version=permission_version+1 WHERE id="+q(sid)+" AND status='suspended' RETURNING id;")
  owner_assert(bool(row),'reenable is only suspended->active; it is not deleted recovery')
 else:
  row=db.sql('tenant',"UPDATE tenant.tenants SET status='active',permission_version=permission_version+1 WHERE id="+q(sid)+" AND status='deleted' AND "+q(NOW)+"::timestamptz < restore_until RETURNING id;")
  owner_assert(bool(row),'deleted recovery requires actual deletion+15-day unexpired window')
 assert db.sql('fabric','SELECT count(*) FROM fabric.resources;')==before
 row=db.one_json('tenant','SELECT status,permission_version,deleted_at,restore_until FROM tenant.tenants WHERE id='+q(sid));assert row['status']=='active' and row['permission_version']==5
 return {'action':action,'finalStatus':row['status'],'permissionVersion':row['permission_version'],'windowOrigin':'actual deleted_at' if action=='restore' else 'no deletion window needed','providerResourcesCreated':0}


def legacy_descriptor_case(db,pb,case):
 sid=case['id'];runtime=copy.deepcopy(PUBLISHER['examples'][0]);revision=copy.deepcopy(runtime['applicationRevisionTemplate']);artifact=copy.deepcopy(runtime['image'])
 descriptor={'schemaVersion':'opl-deployment-descriptor/v1','artifact':artifact,'provenance':'legacy_application','legacyApplicationRevisionId':'original-revision:'+sid,'applicationRevision':revision}
 if case.get('mutation')=='fake_build':descriptor['runtimeContract']=runtime
 api_validate('DeploymentDescriptor',descriptor)
 raw=json.dumps(descriptor,separators=(',',':')).encode();sha=digest(raw);ref='fixture-legacy-descriptor:'+sha;STORAGE[ref]=raw
 data={'id':sid,'version_label':'original-version','artifact_repository':artifact['repository'],'artifact_digest':artifact['digest'],'model_requirements':[],'data_compatibility':{'dataSchemaVersion':'v1','compatibleFromVersions':[],'rollbackSafe':True,'migrationRequired':False},'provenance_evidence':{'source':'exact-original-revision'},'provenance':'legacy_application','legacy_application_revision_id':descriptor['legacyApplicationRevisionId'],'deployment_descriptor':descriptor,'deployment_descriptor_digest':sha,'deployment_descriptor_object_ref':ref}
 db.insert('capability','capability_versions',**data)
 row=db.one_json('capability','SELECT package_id,package_version_id,build_job_id,runtime_version_id,webui_version_id,deployment_descriptor FROM capability.capability_versions WHERE id='+q(sid))
 assert all(row[k] is None for k in ['package_id','package_version_id','build_job_id','runtime_version_id','webui_version_id']) and row['deployment_descriptor']==descriptor
 return {'legacyRevisionId':descriptor['legacyApplicationRevisionId'],'fakeBuildOrPublisherRows':0,'canonicalApplicationRevisionPreserved':True}


def renewal_consent_case(db,pb,case):
 sid=case['id'];ws='ws-'+sid;seed_workspace(db,ws,kind='legacy_resource_only')
 db.insert('workspace','subscriptions',id=sid,workspace_id=ws,billing_subject_ref='original-wallet-binding',current_period_start='2026-09-01T00:00:00Z',current_period_end='2026-10-01T00:00:00Z',period_months=1,provenance='legacy_import',legacy_purchase_id='old-purchase:'+sid,legacy_obligation_snapshot={'renewalMode':'automatic','consentId':'old-consent:'+sid},renewal_mode='automatic',renewal_consent_id='old-consent:'+sid,renewal_consent_snapshot={'consentId':'old-consent:'+sid,'source':'original-verified-consent'},renewal_settings_version=7)
 if case.get('mutation')=='missing_consent':
  try:db.sql('workspace','UPDATE workspace.subscriptions SET renewal_consent_id=NULL,renewal_consent_snapshot=NULL WHERE id='+q(sid)+';')
  except PgError as e:
   if e.state=='23514':raise Rejection('database_reject','automatic mode requires exact consent identity and snapshot')
   raise
  raise AssertionError('missing automatic consent accepted')
 if case.get('mutation')=='cancel':
  request={'renewalMode':'manual','expectedRenewalSettingsVersion':'7'};api_validate('UpdateRenewalSettingsRequest',request)
  updated=db.sql('workspace',"UPDATE workspace.subscriptions SET renewal_mode='manual',renewal_settings_version=8 WHERE id="+q(sid)+' AND renewal_settings_version=7 RETURNING id;')
  owner_assert(bool(updated),'renewal settings stale version')
 row=db.one_json('workspace','SELECT renewal_mode,renewal_consent_id,renewal_consent_snapshot,renewal_settings_version FROM workspace.subscriptions WHERE id='+q(sid))
 assert row['renewal_consent_id']=='old-consent:'+sid
 assert row['renewal_mode']==('manual' if case.get('mutation')=='cancel' else 'automatic')
 return {'mode':row['renewal_mode'],'originalConsentRetained':True,'settingsVersion':row['renewal_settings_version'],'newWalletCharges':0}



def unix_millis(value):
 m=re.fullmatch(r'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(?:Z|\+00:00)',value)
 if not m:raise Rejection('owner_reject','billing clock must be an exact UTC instant')
 dt=datetime.strptime(m[1],'%Y-%m-%dT%H:%M:%S');return calendar.timegm(dt.timetuple())*1000+int((m[2] or '').ljust(3,'0')[:3])
def millis_text(value):
 sec,ms=divmod(value,1000);return datetime.fromtimestamp(sec,timezone.utc).strftime('%Y-%m-%dT%H:%M:%S')+f'.{ms:03d}Z'
def next_billing_month(value,anchor_day):
 # Exact existing Owner rule: monthly_billing.go nextBillingMonth, original
 # anchorDay is retained across clipped February. No new PlanChange calendar.
 m=re.fullmatch(r'(\d{4})-(\d{2})-(\d{2})(T.*)',value);year,month=int(m[1]),int(m[2]);month+=1
 if month==13:year+=1;month=1
 return f'{year:04d}-{month:02d}-{min(anchor_day,calendar.monthrange(year,month)[1]):02d}'+m[4]
def billing_anchor_case(db,pb,case):
 first=next_billing_month(case['start'],case['anchorDay']);second=next_billing_month(first,case['anchorDay'])
 assert first==case['expectedNext'] and second==case['expectedFollowing']
 return {'ownerRule':'monthly_billing.go:nextBillingMonth; no clipped-day overwrite','next':first,'following':second,'originalAnchorDay':case['anchorDay']}

def plan_catalog(db,sid,cpu,memory,storage,monthly,family='standard'):
 comp='compute-'+sid;store='storage-'+sid;price='price-'+sid
 db.insert('resource_catalog','compute_plans',id=comp,name=comp,version_label='v1',provider='local',provider_profile_ref='test-profile',region='local',status='approved',billing_mode='LOCAL_NO_CHARGE',valid_from='2026-01-01T00:00:00Z',published_by='test-admin',provider_capability_version='test-resource-transition/v1',provider_specification={'family':family,'performanceClass':'general'},vcpus=cpu,memory_mib=memory)
 db.insert('resource_catalog','storage_plans',id=store,name=store,version_label='v1',provider='local',provider_profile_ref='test-profile',region='local',status='approved',billing_mode='LOCAL_NO_CHARGE',valid_from='2026-01-01T00:00:00Z',published_by='test-admin',provider_capability_version='test-resource-transition/v1',provider_specification={'family':'persistent'},capacity_gib=storage,shrink_supported=False)
 request={'versionLabel':'v1','periodMonths':1,'computeMonthlyUSDMicros':str(monthly),'storageMonthlyUSDMicros':'0','productMonthlyUSDMicros':'0','validFrom':'2026-01-01T00:00:00Z','computePlanId':comp,'storagePlanId':store,'renewalPolicy':{'version':'renewal-policy/v1','trigger':'manual_or_explicitly_consented_automatic','effectiveStart':'previous_paid_through','months':1,'usesAcceptedPriceSnapshot':True},'planChangePolicyVersion':'workspace-plan-change-v1'}
 api_validate('CreatePricePolicyRequest',request)
 db.insert('resource_catalog','price_policy_versions',id=price,version_label='v1',compute_plan_id=comp,storage_plan_id=store,compute_monthly_usd_micros=monthly,storage_monthly_usd_micros=0,product_monthly_usd_micros=0,currency='USD',plan_change_policy_version=request['planChangePolicyVersion'],renewal_rules=request['renewalPolicy'],valid_from=request['validFrom'],published_by='test-admin',period_months=1)
 return comp,store,price

def seed_plan_source(db,case):
 sid=case['id'];ws='plan-ws-'+sid;sub='plan-sub-'+sid;period='plan-period-'+sid;old=money(case['sourceMonthlyUSDMicros'])
 comp,store,price=plan_catalog(db,'source-'+sid,2,4096,20,old)
 db.insert('workspace','workspaces',id=ws,tenant_id='test-tenant',name=ws,status='active',compute_plan_id=comp,storage_plan_id=store,capability_version_id='capability-base',created_by='test-actor',delivery_model='agent_saas',version=1)
 if not case.get('resourceOnly'):
  db.insert('workspace','operations',id='source-app-operation:'+sid,tenant_id='test-tenant',actor_id='test-actor',kind='create_workspace',resource_id=ws,status='succeeded',stage='succeeded',request_id='source-app-request:'+sid,accepted_input={'source':'preexisting fixture binding'},completed_at=NOW)
  db.insert('serve','agent_deployments',id='source-app-deployment:'+sid,workspace_id=ws,capability_version_id='capability-base',artifact_digest='sha256:'+'2'*64,reference_claim_id='source-app-claim:'+sid,runtime_instance_id='source-runtime:'+sid,operation_id='source-app-operation:'+sid,status='active',data_compatibility={'dataSchemaVersion':'v1'},verification_evidence_ref='fixture-runtime-ready:'+sid,activated_at=NOW,execution_epoch=2)
 else:
  db.sql('workspace',"UPDATE workspace.workspaces SET delivery_model='legacy_resource_only',capability_version_id=NULL WHERE id="+q(ws)+';')
 db.insert('workspace','subscriptions',id=sub,workspace_id=ws,billing_subject_ref='plan-wallet-'+sid,current_period_start=case['periodStart'],current_period_end=case['periodEnd'],period_months=1,provenance='legacy_import',legacy_purchase_id='original:'+sid,legacy_obligation_snapshot={'originalPeriodStart':case['periodStart'],'originalPeriodEnd':case['periodEnd'],'originalPurchaseId':'original:'+sid},renewal_mode='manual',version=1,current_price_policy_version_id=price,current_monthly_usd_micros=old,billing_anchor_day=case.get('anchorDay',int(case['periodEnd'][8:10])))
 db.insert('workspace','subscription_periods',id=period,subscription_id=sub,period_start=case['periodStart'],period_end=case['periodEnd'],billing_key='base-period-key:'+sid,charge_receipt_id='base-receipt:'+sid,provenance='legacy_import',legacy_purchase_id='original:'+sid,legacy_obligation_snapshot={'originalPeriodStart':case['periodStart'],'originalPeriodEnd':case['periodEnd']})
 db.insert('gateway','tenant_wallet_bindings',id='plan-wallet-'+sid,tenant_id='test-tenant-'+sid,billing_sub2api_user_id='original-subject-'+sid,delegation_ref='confirmed-fixture-delegation',verification_evidence_ref='fixture-identity-receipt',bound_at=NOW)
 db.insert('fabric','resource_sets',id='plan-set-'+sid,tenant_id='test-tenant',workspace_id=ws,provider='local',provider_profile_ref='test-profile',region='local',compute_plan_id=comp,storage_plan_id=store,accepted_quote_id='original-quote:'+sid,approved_specification={'scope':'isolated-plan-test'},observation_result='confirmed',observed_at=NOW)
 return {'workspace':ws,'subscription':sub,'period':period,'set':'plan-set-'+sid,'wallet':'plan-wallet-'+sid,'caseId':sid}

def make_plan(db,pb,case,ctx,suffix='',force_kind=None,force_target=None,force_time=None):
 sid=case['id']+suffix;sub=db.one_json('workspace','SELECT * FROM workspace.subscriptions WHERE id='+q(ctx['subscription']))
 future=db.one_json('workspace','SELECT id FROM workspace.subscription_period_obligations WHERE subscription_id='+q(ctx['subscription'])+" AND status IN ('accepted','confirmed') AND period_start>="+q(sub['current_period_end'])+'::timestamptz LIMIT 1');owner_assert(future is None,'FUTURE_PERIOD_COMMITTED: do not reprice a different accepted next period')
 ws=db.one_json('workspace','SELECT * FROM workspace.workspaces WHERE id='+q(ctx['workspace']))
 owner_assert(sub['current_monthly_usd_micros'] is not None and sub['current_price_policy_version_id'] is not None,'POLICY_UNCONFIGURED: missing authoritative legacy accepted price')
 source_cpu=db.one_json('resource_catalog','SELECT vcpus,memory_mib,provider_specification FROM resource_catalog.compute_plans WHERE id='+q(ws['compute_plan_id']));source_storage=db.one_json('resource_catalog','SELECT capacity_gib FROM resource_catalog.storage_plans WHERE id='+q(ws['storage_plan_id']))
 downward=force_kind=='downgrade_next_period';dims=(source_cpu['vcpus'],source_cpu['memory_mib'],source_storage['capacity_gib']);target=(max(1,dims[0]//2),max(1024,dims[1]//2),dims[2]) if downward else (dims[0]*2,dims[1]*2,dims[2]);mutation=case.get('transitionMutation')
 if mutation=='no_op':target=dims
 if mutation=='mixed':target=(dims[0]*2,dims[1]//2,dims[2])
 if mutation=='shrink':target=(dims[0],dims[1],dims[2]//2)
 owner_assert(target!=dims,'PLAN_CHANGE_NO_OP')
 up=all(a>=b for a,b in zip(target,dims));down=all(a<=b for a,b in zip(target,dims));owner_assert(up or down,'MIXED_TRANSITION_REJECTED')
 owner_assert(target[2]>=dims[2],'STORAGE_SHRINK_UNSUPPORTED')
 owner_assert(mutation!='incomparable','resource family transition has no approved Fabric/Catalog receipt')
 kind='upgrade_immediate' if up else 'downgrade_next_period';new=money(force_target if force_target is not None else (case['targetMonthlyUSDMicros'] if kind=='upgrade_immediate' else '10000000'))
 S=(sub['legacy_obligation_snapshot']['originalPeriodStart'] if sub['provenance']=='legacy_import' else sub['accepted_quote_snapshot']['periodStart']);E=(sub['legacy_obligation_snapshot']['originalPeriodEnd'] if sub['provenance']=='legacy_import' else sub['accepted_quote_snapshot']['periodEnd']);T=force_time or case['quoteAt'];sm,em,tm=unix_millis(S),unix_millis(E),unix_millis(T);D=em-sm;R=em-tm
 owner_assert(D>0 and 0<R<=D and sm<=tm<em,'NO_BILLABLE_REMAINING_PERIOD_OR_INVALID_CLOCK')
 T=millis_text(tm);source_snapshot={'schemaVersion':1,'subscriptionId':ctx['subscription'],'subscriptionVersion':str(sub['version']),'periodId':ctx['period'],'computePlanId':ws['compute_plan_id'],'storagePlanId':ws['storage_plan_id'],'acceptedPricePolicyVersionId':sub['current_price_policy_version_id'],'acceptedMonthlyUsdMicros':str(sub['current_monthly_usd_micros']),'originalPeriodStart':S,'originalPeriodEnd':E,'periodStartMilliseconds':str(sm),'periodEndMilliseconds':str(em),'billingAnchorDay':sub['billing_anchor_day'],'sourceOwnerEvidenceReference':'workspace:'+ctx['subscription']+':version:'+str(sub['version'])};proto_validate(pb,'SourceFinancialSnapshot',source_snapshot);source_bytes=json.dumps(source_snapshot,sort_keys=True,separators=(',',':')).encode();source_digest=digest(source_bytes)
 old=sub['current_monthly_usd_micros'];delta=max(new-old,0);charge=(delta*R+D-1)//D if kind=='upgrade_immediate' else 0;owner_assert(charge<=MAX_INT64,'final int64 monetary overflow')
 comp,store,price=plan_catalog(db,'target-'+sid,*target,new)
 calc={'policyVersion':'workspace-plan-change-v1','kind':kind,'transitionId':'approved-transition:'+sid,'sourceSubscriptionId':ctx['subscription'],'sourceSubscriptionVersion':str(sub['version']),'sourcePeriodId':ctx['period'],'sourceComputePlanId':ws['compute_plan_id'],'sourceStoragePlanId':ws['storage_plan_id'],'targetComputePlanId':comp,'targetStoragePlanId':store,'sourcePricePolicyVersionId':sub['current_price_policy_version_id'],'targetPricePolicyVersionId':price,'sourceMonthlyUSDMicros':str(old),'targetMonthlyUSDMicros':str(new),'quoteAt':T,'periodStart':S,'periodEnd':E,'chargeUSDMicros':str(charge),'plannedEffectiveAt':T if kind=='upgrade_immediate' else E}
 if kind=='upgrade_immediate':calc['upgradeProration']={'periodMilliseconds':str(D),'remainingMilliseconds':str(R),'priceDeltaUSDMicros':str(delta),'rounding':'ceil_once_usd_micro'}
 else:calc['nextPeriod']={'periodStart':E,'periodEnd':next_billing_month(E,sub['billing_anchor_day']),'totalUSDMicros':str(new),'pricePolicyVersionId':price}
 execution_plan={'strategy':'in_place_resize','workspaceId':ctx['workspace'],'sourceComputePlanId':ws['compute_plan_id'],'sourceStoragePlanId':ws['storage_plan_id'],'targetComputePlanId':comp,'targetStoragePlanId':store,'transitionId':calc['transitionId']}
 plan_bytes=json.dumps(execution_plan,sort_keys=True,separators=(',',':')).encode();plan_digest=digest(plan_bytes);plan_id='execution-plan:'+sid
 db.sql('fabric',"INSERT INTO fabric.resource_actions(id,resource_set_id,command_id,action,provider_idempotency_key,approved_input,observation_result,evidence_ref,execution_plan_digest,execution_plan_bytes) VALUES ("+','.join([q(plan_id),q(ctx['set']),q('preflight:'+sid),q('prepare_resize'),q('preflight:'+sid),j({'executionPlan':execution_plan}),q('confirmed'),q('preflight-approval:'+sid),q(plan_digest),"decode("+q(plan_bytes.hex())+",'hex')"])+");")
 calc['executionPlanId']=plan_id;calc['executionPlanDigest']=plan_digest;calc.update(quoteAtMilliseconds=str(tm),periodStartMilliseconds=str(sm),periodEndMilliseconds=str(em),sourceFinancialSnapshotDigest=source_digest)
 api_validate('PlanChangeCalculation',calc)
 quote='plan-quote-'+sid;operation='plan-operation-'+sid;pc='plan-change-'+sid
 db.insert('resource_catalog','quotes',id=quote,tenant_id='test-tenant',actor_id='test-actor',purpose='resize',workspace_id=ctx['workspace'],capability_version_id=ws['capability_version_id'],compute_plan_id=comp,storage_plan_id=store,price_policy_version_id=price,refund_policy_version_id='test-refund',retention_policy_version_id='test-retention',total_usd_micros=charge,period_start=S,period_end=E,input_digest=digest(json.dumps(calc,sort_keys=True).encode()),admission_snapshot={'transitionId':calc['transitionId']},expires_at=millis_text(tm+600000),model_selections=[],period_months=1,refund_terms='base policy preserved; supplement coverage policy v1',retention_terms='original storage contract',expected_interruption='approved provider drain required',plan_change_calculation=calc,source_subscription_version=sub['version'])
 if kind=='upgrade_immediate':
  line={'kind':'product','description':'本期套餐升级补差','quantity':1,'amountUSDMicros':str(charge)};api_validate('QuoteLine',line)
  db.insert('resource_catalog','quote_items',id='plan-line:'+sid,quote_id=quote,kind=line['kind'],description=line['description'],amount_usd_micros=charge,calculation={'policyVersion':'workspace-plan-change-v1','oneAggregateCeil':True},sort_order=0,quantity=1)
 persisted_total=db.sql('resource_catalog',"SELECT coalesce(sum(CASE WHEN kind='adjustment_credit' THEN -amount_usd_micros ELSE amount_usd_micros END),0)::text FROM resource_catalog.quote_items WHERE quote_id="+q(quote)+';');assert persisted_total==str(charge)
 if case.get('mutation')=='stale_source':db.sql('workspace','UPDATE workspace.subscriptions SET version=version+1 WHERE id='+q(ctx['subscription'])+';')
 current=db.one_json('workspace','SELECT version FROM workspace.subscriptions WHERE id='+q(ctx['subscription']));owner_assert(current['version']==int(calc['sourceSubscriptionVersion']),'VERSION_CONFLICT: source subscription changed after quote')
 owner_assert(case.get('mutation')!='expired_quote','QUOTE_EXPIRED before accepting new identity')
 db.insert('workspace','operations',id=operation,tenant_id='test-tenant',actor_id='test-actor',kind='resize_workspace',resource_id=pc,status='accepted',stage='accepted',request_id='request-'+sid,accepted_input={'quoteId':quote})
 data={'id':pc,'workspace_id':ctx['workspace'],'tenant_id':'test-tenant','kind':kind,'status':'requested' if kind=='upgrade_immediate' else 'scheduled','source_compute_plan_id':calc['sourceComputePlanId'],'source_storage_plan_id':calc['sourceStoragePlanId'],'target_compute_plan_id':comp,'target_storage_plan_id':store,'source_price_policy_version_id':calc['sourcePricePolicyVersionId'],'target_price_policy_version_id':price,'source_subscription_id':ctx['subscription'],'source_subscription_version':sub['version'],'source_period_id':ctx['period'],'quote_id':quote,'policy_version':'workspace-plan-change-v1','quote_at':T,'period_start':S,'period_end':E,'source_monthly_usd_micros':old,'target_monthly_usd_micros':new,'charge_usd_micros':charge,'planned_effective_at':calc['plannedEffectiveAt'],'operation_id':operation,'execution_plan_id':plan_id,'execution_plan_digest':plan_digest,'execution_operation_id':operation if kind=='upgrade_immediate' else None,'observation_result':'confirmed','accepted_calculation':calc,'quote_at_ms':tm,'period_start_ms':sm,'period_end_ms':em,'source_financial_snapshot_digest':source_digest,'source_financial_snapshot_bytes':source_bytes,'actual_outcome':{'deliveryOutcome':'pending','resourceOutcome':'unchanged','runtimeReadbackRequirement':'required' if db.one_json('serve','SELECT count(*)::int AS n FROM serve.agent_deployments WHERE workspace_id='+q(ws["id"] if isinstance(ws,dict) else ctx["workspace"]) + " AND status='active'")['n'] else 'not_applicable','currentRequirementValidation':'valid'}}
 if kind=='downgrade_next_period':data.update(next_period_start=calc['nextPeriod']['periodStart'],next_period_end=calc['nextPeriod']['periodEnd'],next_period_charge_usd_micros=new)
 try:db.insert('workspace','plan_changes',**data)
 except PgError as e:
  if e.state=='23505':raise Rejection('database_reject','only one unfinished PlanChange per Workspace or duplicate original Quote')
  raise
 db.sql('resource_catalog',"UPDATE resource_catalog.quotes SET status='accepted',accepted_at="+q(T)+',accepted_by_operation_id='+q(operation)+' WHERE id='+q(quote)+" AND status='offered';")
 db.insert('workspace','idempotency_records',id='idem-'+sid,tenant_scope='test-tenant',actor_scope='test-actor',operation_name='resizeWorkspace',idempotency_key='intent-'+sid,request_sha256=hashlib.sha256(json.dumps({'quoteId':quote},sort_keys=True).encode()).hexdigest(),resource_id=pc,operation_id=operation,response_status=202,response_body={'operationId':operation,'resourceId':pc})
 return data,calc


def record_plan_charge(db,ctx,pc,amount,status='confirmed',key=None,ident=None):
 if amount==0:return None
 identity=ident or 'supplement-charge-'+pc['id'];business=key or 'supplement:'+pc['id']
 db.insert('gateway','wallet_operations',id=identity,tenant_id='test-tenant',wallet_binding_id=ctx['wallet'],workspace_id=ctx['workspace'],kind='charge',status=status,amount_usd_micros=amount,business_idempotency_key=business,request_fingerprint=digest(str(amount).encode()),external_reference='external:'+business if status=='confirmed' else None,confirmed_at=NOW if status=='confirmed' else None)
 return identity

def apply_upgrade(db,ctx,pc,calc,case):
 mutation=case.get('mutation');charge=record_plan_charge(db,ctx,pc,pc['charge_usd_micros'],'unknown' if mutation=='unknown_charge' else 'confirmed')
 if charge:db.sql('workspace','UPDATE workspace.plan_changes SET charge_operation_id='+q(charge)+' WHERE id='+q(pc['id'])+';')
 if mutation=='unknown_charge':return {'chargeId':charge,'providerEffects':0,'applied':False}
 if mutation=='unknown_resource':
  db.sql('workspace',"UPDATE workspace.plan_changes SET status='needs_attention',observation_result='unknown',actual_outcome="+j({'deliveryOutcome':'unknown','resourceOutcome':'unknown'})+' WHERE id='+q(pc['id'])+';');return {'chargeId':charge,'providerEffects':0,'applied':False}
 before=db.one_json('workspace','SELECT current_period_start::text AS s,current_period_end::text AS e FROM workspace.subscriptions WHERE id='+q(ctx['subscription']))
 db.insert('fabric','resource_actions',id='resize-effect-'+pc['id'],resource_set_id=ctx['set'],command_id='resize-command-'+pc['id'],action='resize',provider_idempotency_key='provider-resize:'+pc['id'],approved_input=calc,authorization_receipt_ref='bounded-fixture-authorization',observation_result='rejected' if mutation=='partial_failure' else 'confirmed',evidence_ref='resource-evidence:'+pc['id'],execution_epoch=2)
 if mutation=='partial_failure':
  db.sql('workspace',"UPDATE workspace.plan_changes SET status='needs_attention',observation_result='confirmed',actual_outcome="+j({'deliveryOutcome':'failed','resourceOutcome':'irreversible_residual','targetActivated':False,'fenceReceiptId':'late-worker-fenced','failureReceiptId':'confirmed-target-failure'})+' WHERE id='+q(pc['id'])+';');return {'chargeId':charge,'providerEffects':1,'applied':False}
 applied=millis_text(unix_millis(pc['quote_at'])+120000)
 sql='BEGIN; SELECT id FROM workspace.workspaces WHERE id='+q(ctx['workspace'])+' FOR UPDATE; DO $apply$ DECLARE n integer; BEGIN UPDATE workspace.subscriptions SET current_price_policy_version_id='+q(pc['target_price_policy_version_id'])+',current_monthly_usd_micros='+str(pc['target_monthly_usd_micros'])+',version=version+1 WHERE id='+q(ctx['subscription'])+' AND version='+str(pc['source_subscription_version'])+"; GET DIAGNOSTICS n=ROW_COUNT; IF n<>1 THEN RAISE EXCEPTION 'stale source pricing CAS' USING ERRCODE='40001'; END IF; UPDATE workspace.workspaces SET compute_plan_id="+q(pc['target_compute_plan_id'])+',storage_plan_id='+q(pc['target_storage_plan_id'])+',version=version+1 WHERE id='+q(ctx['workspace'])+"; UPDATE workspace.plan_changes SET status='applied',applied_at="+q(applied)+',actual_outcome='+j({'deliveryOutcome':'applied','resourceOutcome':'target_confirmed'})+' WHERE id='+q(pc['id'])+'; END $apply$; COMMIT;'
 try:db.sql('workspace',sql)
 except PgError as e:
  if e.state=='40001':raise Rejection('owner_reject','source CAS failed; no target/current-price commit occurred')
  raise
 db.insert('workspace','supplemental_charges',id='supplement-'+pc['id'],plan_change_id=pc['id'],subscription_period_id=ctx['period'],workspace_id=ctx['workspace'],quote_id=pc['quote_id'],policy_version='workspace-plan-change-v1',coverage_start=pc['quote_at'],coverage_end=pc['period_end'],confirmed_amount_usd_micros=pc['charge_usd_micros'],coverage_start_ms=pc['quote_at_ms'],coverage_end_ms=pc['period_end_ms'],source_financial_snapshot_digest=pc['source_financial_snapshot_digest'],original_wallet_operation_id=charge,charge_receipt_id='supplement-receipt:'+pc['id'],confirmed_at=applied)
 after=db.one_json('workspace','SELECT current_period_start::text AS s,current_period_end::text AS e FROM workspace.subscriptions WHERE id='+q(ctx['subscription']));assert after==before
 return {'chargeId':charge,'providerEffects':1,'applied':True,'originalPeriodExactUnchanged':True,'appliedAt':applied}

def next_period_obligation(db,ctx,pc,actor='manual',accept=False,clock=None):
 clock=clock or pc['next_period_start'];ident='period-obligation:'+pc['id'];billing='renew:'+ctx['subscription']+':'+str(unix_millis(pc['next_period_start']));fund_quote='renew-quote:'+pc['id']+':'+actor;op='renew-operation:'+pc['id']
 snapshot={'quoteId':fund_quote,'sourcePlanQuoteId':pc['quote_id'],'computePlanId':pc['target_compute_plan_id'],'storagePlanId':pc['target_storage_plan_id'],'pricePolicyVersionId':pc['target_price_policy_version_id'],'totalUSDMicros':str(pc['next_period_charge_usd_micros'])}
 quote={'id':fund_quote,'tenant_id':'test-tenant','actor_id':'test-actor','purpose':'renew','workspace_id':ctx['workspace'],'capability_version_id':'capability-base','compute_plan_id':pc['target_compute_plan_id'],'storage_plan_id':pc['target_storage_plan_id'],'price_policy_version_id':pc['target_price_policy_version_id'],'refund_policy_version_id':'test-refund','retention_policy_version_id':'test-retention','total_usd_micros':pc['next_period_charge_usd_micros'],'period_start':pc['next_period_start'],'period_end':pc['next_period_end'],'input_digest':digest(json.dumps(snapshot,sort_keys=True).encode()),'admission_snapshot':{'scheduledPlanChangeId':pc['id']},'expires_at':millis_text(unix_millis(clock)+600000),'model_selections':[],'period_months':1,'refund_terms':'original target-period policy','retention_terms':'original retention','expected_interruption':'approved transition','source_subscription_version':pc['source_subscription_version'],'scheduled_plan_change_id':pc['id']}
 db.sql('resource_catalog',sql_insert('resource_catalog.quotes',quote).rstrip(';')+' ON CONFLICT (id) DO NOTHING;')
 price=db.one_json('resource_catalog','SELECT compute_monthly_usd_micros,storage_monthly_usd_micros,product_monthly_usd_micros FROM resource_catalog.price_policy_versions WHERE id='+q(pc['target_price_policy_version_id']))
 for order,kind in enumerate(['compute','storage','product']):
  amount=price[kind+'_monthly_usd_micros'];line={'id':fund_quote+':'+kind,'quote_id':fund_quote,'kind':kind,'description':'已接受目标套餐'+kind+'月费','amount_usd_micros':amount,'calculation':{'pricePolicyVersionId':pc['target_price_policy_version_id']},'sort_order':order,'quantity':1}
  db.sql('resource_catalog',sql_insert('resource_catalog.quote_items',line).rstrip(';')+' ON CONFLICT (id) DO NOTHING;')
 assert sum(price.values())==pc['next_period_charge_usd_micros']
 data={'id':ident+'-'+actor,'subscription_id':ctx['subscription'],'workspace_id':ctx['workspace'],'plan_change_id':pc['id'],'period_start':pc['next_period_start'],'period_end':pc['next_period_end'],'target_compute_plan_id':pc['target_compute_plan_id'],'target_storage_plan_id':pc['target_storage_plan_id'],'target_price_policy_version_id':pc['target_price_policy_version_id'],'amount_usd_micros':pc['next_period_charge_usd_micros'],'accepted_pricing_snapshot':snapshot,'status':'awaiting_payment','billing_key':billing,'quote_id':fund_quote}
 insert=sql_insert('workspace.subscription_period_obligations',data).rstrip(';')+' ON CONFLICT (subscription_id,period_start) DO NOTHING;'
 db.sql('workspace','BEGIN; SELECT id FROM workspace.workspaces WHERE id='+q(ctx['workspace'])+' FOR UPDATE; SELECT id FROM workspace.subscriptions WHERE id='+q(ctx['subscription'])+' FOR UPDATE; '+insert+' COMMIT;')
 ob=db.one_json('workspace','SELECT * FROM workspace.subscription_period_obligations WHERE subscription_id='+q(ctx['subscription'])+' AND period_start='+q(pc['next_period_start'])+'::timestamptz');db.sql('workspace','UPDATE workspace.plan_changes SET next_period_obligation_id='+q(ob['id'])+' WHERE id='+q(pc['id'])+';')
 if accept:
  operation={'id':op,'tenant_id':'test-tenant','actor_id':'test-actor','kind':'renew_workspace','resource_id':ob['id'],'status':'accepted','stage':'accepted','request_id':'renew-request:'+pc['id'],'accepted_input':{'periodObligationId':ob['id'],'quoteId':ob['quote_id']}}
  guard="IF NOT EXISTS (SELECT 1 FROM workspace.plan_changes WHERE id="+q(pc['id'])+" AND status IN ('scheduled','awaiting_payment','applying')) THEN RAISE EXCEPTION 'scheduled intent cancelled' USING ERRCODE='40001'; END IF;"
  sql='BEGIN; SELECT id FROM workspace.workspaces WHERE id='+q(ctx['workspace'])+' FOR UPDATE; SELECT id FROM workspace.subscriptions WHERE id='+q(ctx['subscription'])+' FOR UPDATE; DO $accept$ BEGIN '+guard+' END $accept$; '+sql_insert('workspace.operations',operation).rstrip(';')+' ON CONFLICT (id) DO NOTHING; UPDATE workspace.subscription_period_obligations SET status=\'accepted\',operation_id='+q(op)+',payment_accepted_at='+q(clock)+',version=version+1 WHERE id='+q(ob['id'])+' AND payment_accepted_at IS NULL; COMMIT;'
  try:db.sql('workspace',sql)
  except PgError as e:
   if e.state=='40001':raise Rejection('owner_reject','cancel won before financial acceptance; no Gateway call')
   raise
  db.sql('resource_catalog',"UPDATE resource_catalog.quotes SET status='accepted',accepted_at="+q(clock)+',accepted_by_operation_id='+q(op)+' WHERE id='+q(ob['quote_id'])+" AND status='offered';")
  walletid='next-payment:'+pc['id']+'-'+actor;key=ob['billing_key'];values={'id':walletid,'tenant_id':'test-tenant','wallet_binding_id':ctx['wallet'],'workspace_id':ctx['workspace'],'kind':'charge','status':'requested','amount_usd_micros':ob['amount_usd_micros'],'business_idempotency_key':key,'request_fingerprint':digest(json.dumps(ob['accepted_pricing_snapshot'],sort_keys=True).encode()),'external_reference':None,'confirmed_at':None,'purpose':'base_period','plan_change_id':pc['id'],'coverage_start':pc['next_period_start'],'coverage_end':pc['next_period_end']}
  inserted=db.sql('gateway',sql_insert('gateway.wallet_operations',values).rstrip(';')+' ON CONFLICT (business_idempotency_key) DO NOTHING RETURNING id;')
  if inserted:
   db.sql('gateway',"UPDATE gateway.wallet_operations SET status='confirmed',external_reference="+q('external:'+key)+',confirmed_at='+q(clock)+' WHERE id='+q(walletid)+" AND status='requested';")
  charged=db.one_json('gateway','SELECT id,amount_usd_micros FROM gateway.wallet_operations WHERE business_idempotency_key='+q(key));owner_assert(charged['amount_usd_micros']==pc['next_period_charge_usd_micros'],'same next-period key cannot silently use another price')
  # Advance the financial version exactly once, retain original current S/E until boundary application.
  sql='BEGIN; SELECT id FROM workspace.workspaces WHERE id='+q(ctx['workspace'])+' FOR UPDATE; SELECT id FROM workspace.subscriptions WHERE id='+q(ctx['subscription'])+' FOR UPDATE; DO $paid$ DECLARE v bigint; BEGIN IF EXISTS (SELECT 1 FROM workspace.subscription_period_obligations WHERE id='+q(ob['id'])+' AND confirmed_at IS NULL) THEN UPDATE workspace.subscriptions SET version=version+1 WHERE id='+q(ctx['subscription'])+' RETURNING version INTO v; UPDATE workspace.subscription_period_obligations SET status=\'confirmed\',wallet_operation_id='+q(charged['id'])+',confirmed_at='+q(clock)+',confirmed_subscription_version=v,version=version+1 WHERE id='+q(ob['id'])+'; END IF; END $paid$; COMMIT;';db.sql('workspace',sql)
  ob=db.one_json('workspace','SELECT * FROM workspace.subscription_period_obligations WHERE id='+q(ob['id']))
 return ob

def cancel_plan(db,ctx,pc,for_delete=False):
 row=db.one_json('workspace','SELECT * FROM workspace.plan_changes WHERE id='+q(pc['id']))
 owner_assert(row['kind']=='downgrade_next_period' and row['status'] in ['scheduled','awaiting_payment'],'plan executing/failed-needs_attention must converge before cancellation/deletion')
 ob=db.one_json('workspace','SELECT payment_accepted_at,wallet_operation_id,resource_execution_started_at FROM workspace.subscription_period_obligations WHERE plan_change_id='+q(pc['id']))
 owner_assert(ob is None or all(ob[k] is None for k in ob),'cannot cancel after target-period funds or execution are accepted')
 request={'expectedScheduleVersion':str(row['schedule_version']),'reason':'explicit customer cancellation'};api_validate('CancelPlanChangeRequest',request)
 # Resource-level serialization is local and conditional; no cross-owner transaction.
 cancel_id='cancel-operation:'+pc['id'];operation={'id':cancel_id,'tenant_id':'test-tenant','actor_id':'test-actor','kind':'cancel_plan_change','resource_id':pc['id'],'status':'succeeded','stage':'succeeded','request_id':'cancel-request:'+pc['id'],'accepted_input':request,'completed_at':NOW}
 guard="IF NOT EXISTS (SELECT 1 FROM workspace.plan_changes WHERE id="+q(pc['id'])+" AND kind='downgrade_next_period' AND status IN ('scheduled','awaiting_payment') AND schedule_version="+str(row['schedule_version'])+") OR EXISTS (SELECT 1 FROM workspace.subscription_period_obligations WHERE plan_change_id="+q(pc['id'])+" AND (payment_accepted_at IS NOT NULL OR wallet_operation_id IS NOT NULL OR resource_execution_started_at IS NOT NULL)) THEN RAISE EXCEPTION 'payment or execution accepted before cancellation' USING ERRCODE='40001'; END IF;"
 sql='BEGIN; SELECT id FROM workspace.workspaces WHERE id='+q(ctx['workspace'])+' FOR UPDATE; SELECT id FROM workspace.subscriptions WHERE id='+q(ctx['subscription'])+' FOR UPDATE; DO $cancel$ BEGIN '+guard+' END $cancel$; '+sql_insert('workspace.operations',operation)+"UPDATE workspace.plan_changes SET status='cancelled',cancelled_at="+q(NOW)+',schedule_version=schedule_version+1,cancellation_operation_id='+q(cancel_id)+' WHERE id='+q(pc['id'])+';'+(" UPDATE workspace.workspaces SET status='deleting' WHERE id="+q(ctx['workspace'])+';' if for_delete else '')+' COMMIT;'
 try:db.sql('workspace',sql)
 except PgError as e:
  if e.state=='40001':raise Rejection('owner_reject','period funds or execution accepted: cancellation/deletion cannot race through')
  raise
 return {'cancelled':True,'workspaceDeleting':for_delete,'originalQuotesRetained':True}

def refund_original(db,ctx,pc,charge_id,amount,reason,key,status='confirmed'):
 original=db.one_json('gateway','SELECT amount_usd_micros,status FROM gateway.wallet_operations WHERE id='+q(charge_id));owner_assert(original and original['status']=='confirmed','UNKNOWN_OR_UNCONFIRMED_CHARGE_CANNOT_REFUND')
 entitlement={'originalWalletOperationId':charge_id,'planChangeId':pc['id'],'reason':reason,'amountUSDMicros':str(amount),'evidence':'confirmed-original-owner-readback'}
 ident='refund:'+key;data={'id':ident,'tenant_id':'test-tenant','wallet_binding_id':ctx['wallet'],'workspace_id':ctx['workspace'],'kind':'refund','status':status,'amount_usd_micros':amount,'original_wallet_operation_id':charge_id,'business_idempotency_key':key,'request_fingerprint':digest(json.dumps(entitlement,sort_keys=True).encode()),'external_reference':'external:'+key if status=='confirmed' else None,'confirmed_at':NOW if status=='confirmed' else None,'refund_entitlement_ref':'entitlement:'+key,'purpose':reason,'refund_entitlement_snapshot':entitlement}
 stmt=sql_insert('gateway.wallet_operations',data)
 sql='BEGIN; SELECT id FROM gateway.wallet_operations WHERE id='+q(charge_id)+' FOR UPDATE; DO $refund$ DECLARE remaining numeric; existing_amount bigint; BEGIN SELECT amount_usd_micros INTO existing_amount FROM gateway.wallet_operations WHERE business_idempotency_key='+q(key)+'; IF FOUND THEN IF existing_amount <> '+str(amount)+" THEN RAISE EXCEPTION 'refund key input conflict' USING ERRCODE='23514'; END IF; ELSE SELECT original.amount_usd_micros-coalesce((SELECT sum(r.amount_usd_micros) FROM gateway.wallet_operations r WHERE r.original_wallet_operation_id=original.id AND r.kind='refund' AND r.status IN ('requested','unknown','confirmed')),0) INTO remaining FROM gateway.wallet_operations original WHERE original.id="+q(charge_id)+" AND original.kind='charge' AND original.status='confirmed'; IF remaining IS NULL OR "+str(amount)+" > remaining THEN RAISE EXCEPTION 'original charge refundable amount exceeded, including unknown requests' USING ERRCODE='23514'; END IF; "+stmt+' END IF; END $refund$; COMMIT;'
 try:db.sql('gateway',sql)
 except PgError as e:
  if e.state=='23514':raise Rejection('database_reject','refund cannot exceed original charge net of confirmed/requested/unknown refunds')
  raise
 return ident

def plan_projection(db,ctx,pc):
 row=db.one_json('workspace','SELECT * FROM workspace.plan_changes WHERE id='+q(pc['id']));calc=row['accepted_calculation']
 response={k:calc[k] for k in ['kind','sourceComputePlanId','sourceStoragePlanId','targetComputePlanId','targetStoragePlanId','sourcePricePolicyVersionId','targetPricePolicyVersionId','sourceSubscriptionId','sourceSubscriptionVersion','sourcePeriodId','sourceMonthlyUSDMicros','targetMonthlyUSDMicros','policyVersion','quoteAt','periodStart','periodEnd','chargeUSDMicros','plannedEffectiveAt']}
 response.update(id=row['id'],workspaceId=row['workspace_id'],status=row['status'],quoteId=row['quote_id'],operationId=row['operation_id'],chargeStatus='not_required' if row['charge_usd_micros']==0 else 'not_requested',refundOperationIds=[],scheduleVersion=str(row['schedule_version']),observationResult=row['observation_result'],cancellable=row['kind']=='downgrade_next_period' and row['status'] in ['scheduled','awaiting_payment'],createdAt=row['created_at'],updatedAt=row['updated_at'],executionPlanId=row['execution_plan_id'],executionPlanDigest=row['execution_plan_digest'],quoteAtMilliseconds=str(row['quote_at_ms']),periodStartMilliseconds=str(row['period_start_ms']),periodEndMilliseconds=str(row['period_end_ms']),sourceFinancialSnapshotDigest=row['source_financial_snapshot_digest'])
 if row['applied_at']:response['appliedAt']=row['applied_at']
 if row['execution_operation_id']:response['executionOperationId']=row['execution_operation_id']
 if row['charge_operation_id']:
  response['chargeOperationId']=row['charge_operation_id'];w=db.one_json('gateway','SELECT status FROM gateway.wallet_operations WHERE id='+q(row['charge_operation_id']));response['chargeStatus']=w['status']
 if row['kind']=='downgrade_next_period':
  response.update(nextPeriodStart=calc['nextPeriod']['periodStart'],nextPeriodEnd=calc['nextPeriod']['periodEnd'],nextPeriodChargeUSDMicros=str(row['next_period_charge_usd_micros']),nextPeriodChargeStatus='not_requested')
  if row['next_period_obligation_id']:
   response['nextPeriodObligationId']=row['next_period_obligation_id'];ob=db.one_json('workspace','SELECT wallet_operation_id FROM workspace.subscription_period_obligations WHERE id='+q(row['next_period_obligation_id']))
   if ob['wallet_operation_id']:
    response['nextPeriodChargeOperationId']=ob['wallet_operation_id'];w=db.one_json('gateway','SELECT status FROM gateway.wallet_operations WHERE id='+q(ob['wallet_operation_id']));response['nextPeriodChargeStatus']=w['status'];response['cancellable']=False
 if row['actual_outcome']:
  response.update({k:row['actual_outcome'][k] for k in ['deliveryOutcome','resourceOutcome','runtimeReadbackRequirement','currentRequirementValidation'] if k in row['actual_outcome']})
 current_serve=db.one_json('serve','SELECT count(*)::int AS n FROM serve.agent_deployments WHERE workspace_id='+q(ctx['workspace'])+" AND status='active'")
 response.setdefault('runtimeReadbackRequirement','required' if current_serve['n'] else 'not_applicable');response.setdefault('currentRequirementValidation','valid')
 api_validate('PlanChange',response);return response


def plan_change_case(db,pb,case):
 api_validate('PlanChangePolicy',PLAN_POLICY)
 # Invalid-clock rejection precedes any financial or resource side effect.
 S,E,T=(unix_millis(case[k]) for k in ['periodStart','periodEnd','quoteAt']);owner_assert(E>S and S<=T<E,'no billable source period remains')
 ctx=seed_plan_source(db,case);mutation=case.get('mutation');down=mutation in ['scheduled','manual_boundary','boundary_race','cancel','cancel_after_accept','delete_scheduled','renew_target']
 pc,calc=make_plan(db,pb,case,ctx,force_kind='downgrade_next_period' if down else None)
 if not mutation or mutation in ['stale_source','expired_quote']:
  assert str(pc['charge_usd_micros'])==case['expectedChargeUSDMicros']
  if case.get('mathOnly'):return {'calculation':calc,'persistedChargeUSDMicros':str(pc['charge_usd_micros']),'scope':'quote/acceptance numeric boundary, no provider completion-time claim'}
  result=apply_upgrade(db,ctx,pc,calc,case);projection=plan_projection(db,ctx,pc);return {'calculation':calc,'persistedChargeUSDMicros':str(pc['charge_usd_micros']),'projectionStatus':projection['status'],**result}
 if mutation=='second_plan':make_plan(db,pb,case,ctx,suffix='-second');raise AssertionError('second unfinished change accepted')
 if mutation=='delete_applying':
  db.sql('workspace',"UPDATE workspace.plan_changes SET status='applying' WHERE id="+q(pc['id'])+';');return cancel_plan(db,ctx,pc,for_delete=True)
 if mutation=='duplicate':
  result=apply_upgrade(db,ctx,pc,calc,case);prior=db.one_json('workspace','SELECT resource_id,operation_id FROM workspace.idempotency_records WHERE id='+q('idem-'+case['id']));assert prior['resource_id']==pc['id'] and prior['operation_id']==pc['operation_id'];assert db.sql('gateway','SELECT count(*) FROM gateway.wallet_operations WHERE business_idempotency_key='+q('supplement:'+pc['id'])+';')=='1';return {'samePlanChange':pc['id'],'chargeOriginalKeyCount':1,'quoteClockUnchanged':calc['quoteAt'],**result}
 if mutation=='chain':
  first=apply_upgrade(db,ctx,pc,calc,case);pc2,calc2=make_plan(db,pb,case,ctx,suffix='-second',force_target='60000000',force_time='2026-09-23T12:00:00Z');assert calc2['sourceMonthlyUSDMicros']=='40000000' and calc2['chargeUSDMicros']=='5000000';second=apply_upgrade(db,ctx,pc2,calc2,case);return {'firstCharge':'10000000','secondSourcePrice':'40000000','secondCharge':'5000000','originalPeriodEndUnchanged':second['originalPeriodExactUnchanged']}
 if mutation in ['scheduled','manual_boundary','boundary_race','cancel','cancel_after_accept','delete_scheduled','renew_target']:
  before=db.sql('fabric',"SELECT count(*) FROM fabric.resource_actions WHERE action<>'prepare_resize' AND resource_set_id="+q(ctx['set'])+';');assert before=='0'
  source=db.one_json('workspace','SELECT current_period_end::text AS e,current_monthly_usd_micros FROM workspace.subscriptions WHERE id='+q(ctx['subscription']));assert source['current_monthly_usd_micros']==money(case['sourceMonthlyUSDMicros'])
  if mutation=='cancel':return cancel_plan(db,ctx,pc)
  if mutation=='delete_scheduled':return cancel_plan(db,ctx,pc,for_delete=True)
  if mutation=='scheduled':return {'status':plan_projection(db,ctx,pc)['status'],'currentChargeUSDMicros':'0','targetNextPeriodUSDMicros':str(pc['next_period_charge_usd_micros']),'providerEffectsBeforeE':0}
  if mutation=='manual_boundary':
   ob=next_period_obligation(db,ctx,pc,accept=False);db.sql('workspace',"UPDATE workspace.plan_changes SET status='awaiting_payment' WHERE id="+q(pc['id'])+"; UPDATE workspace.workspaces SET status='suspended' WHERE id="+q(ctx['workspace'])+';');assert ob['wallet_operation_id'] is None;return {'status':plan_projection(db,ctx,pc)['status'],'automaticChargeCreated':False,'providerEffects':0}
  if mutation=='boundary_race':
   with ThreadPoolExecutor(max_workers=3) as pool:rows=list(pool.map(lambda actor:next_period_obligation(db,ctx,pc,actor,True),['manual','automatic','boundary']))
   assert len({r['id'] for r in rows})==1 and len({r['wallet_operation_id'] for r in rows})==1
   count=db.sql('gateway','SELECT count(*) FROM gateway.wallet_operations WHERE business_idempotency_key='+q(rows[0]['billing_key'])+';');assert count=='1';return {'concurrentActors':3,'obligationIdentities':1,'gatewayOriginalPayments':1,'nextPeriodTargetAmountUSDMicros':'10000000','oldHighPriceNotCharged':True}
  ob=next_period_obligation(db,ctx,pc,accept=True)
  if mutation=='cancel_after_accept':return cancel_plan(db,ctx,pc)
  charged=db.one_json('gateway','SELECT amount_usd_micros FROM gateway.wallet_operations WHERE id='+q(ob['wallet_operation_id']));assert charged['amount_usd_micros']==10000000;return {'nextPeriodAmountUSDMicros':'10000000','sourceOldPriceUSDMicros':'20000000','oldPriceThenRefundPattern':False}
 if mutation in ['partial_failure','unknown_resource','unknown_charge','supplement_delete','refund_cap']:
  result=apply_upgrade(db,ctx,pc,calc,case)
  if mutation in ['unknown_resource','unknown_charge']:
   count=db.sql('gateway',"SELECT count(*) FROM gateway.wallet_operations WHERE kind='refund' AND workspace_id="+q(ctx['workspace'])+';');assert count=='0';return {'refunds':0,'targetApplied':False,**result}
  if mutation=='partial_failure':
   outcome=db.one_json('workspace','SELECT status,actual_outcome FROM workspace.plan_changes WHERE id='+q(pc['id']));owner_assert(outcome['actual_outcome']['deliveryOutcome']=='failed' and outcome['actual_outcome']['resourceOutcome']=='irreversible_residual' and not outcome['actual_outcome']['targetActivated'],'compensation requires confirmed fenced target failure, not generic needs_attention')
   refund_original(db,ctx,pc,result['chargeId'],10000000,'upgrade_failure_full','failure-refund:'+pc['id']);return {'refundUSDMicros':'10000000','failureNot720Policy':True,'resourceOutcome':'irreversible_residual','planStatus':'needs_attention','targetApplied':False}
  if mutation=='supplement_delete':
   supplement=db.one_json('workspace','SELECT * FROM workspace.supplemental_charges WHERE plan_change_id='+q(pc['id']));delete_at='2026-09-23T12:00:00Z';begin,end=supplement['coverage_start_ms'],supplement['coverage_end_ms'];deleted=unix_millis(delete_at);owner_assert(deleted>=begin and end>begin,'invalid supplemental coverage/deletion')
   amount=supplement['confirmed_amount_usd_micros']*max(end-deleted,0)//(end-begin);assert amount==5000000;refund_original(db,ctx,pc,result['chargeId'],amount,'supplement_delete_unused','delete-refund:'+pc['id']);return {'supplementCoverageStart':calc['quoteAt'],'supplementCoverageEnd':calc['periodEnd'],'deleteAt':delete_at,'refundUSDMicros':'5000000','notBase720':True}
  refund_original(db,ctx,pc,result['chargeId'],7000000,'supplement_delete_unused','unknown-refund:'+pc['id'],status='unknown');refund_original(db,ctx,pc,result['chargeId'],4000000,'supplement_delete_unused','over-cap-refund:'+pc['id']);raise AssertionError('refund cap failed')
 raise AssertionError('unhandled D17 scenario '+str(mutation))



def execute_scheduled_change(db,ctx,pc,clock):
 if unix_millis(clock)<unix_millis(pc['period_end']):return {'state':'scheduled','providerEffects':0,'reason':'before_original_boundary'}
 ob=db.one_json('workspace','SELECT * FROM workspace.subscription_period_obligations WHERE plan_change_id='+q(pc['id']))
 if ob is None or ob['status']!='confirmed':
  db.sql('workspace',"UPDATE workspace.plan_changes SET status='awaiting_payment' WHERE id="+q(pc['id'])+"; UPDATE workspace.workspaces SET status='suspended' WHERE id="+q(ctx['workspace'])+';');return {'state':'awaiting_payment','providerEffects':0}
 sub=db.one_json('workspace','SELECT * FROM workspace.subscriptions WHERE id='+q(ctx['subscription']))
 allowed=sub['version']==pc['source_subscription_version'] or (ob['plan_change_id']==pc['id'] and ob['confirmed_subscription_version']==sub['version'])
 owner_assert(allowed,'STALE_SOURCE: only own confirmed next-period obligation may bridge the source version')
 row=db.one_json('workspace','SELECT * FROM workspace.plan_changes WHERE id='+q(pc['id']));owner_assert(row['status'] in ['scheduled','awaiting_payment'],'plan not executable')
 owner_assert((row['actual_outcome'] or {}).get('currentRequirementValidation')!='at_risk','CURRENT_RUNTIME_REQUIREMENTS_CHANGED: target cannot execute')
 execution='boundary-execution:'+pc['id'];db.insert('workspace','operations',id=execution,tenant_id='test-tenant',actor_id='test-actor',kind='execute_plan_change',resource_id=pc['id'],status='running',stage='resource_resize',request_id='boundary:'+pc['id'],accepted_input={'periodObligationId':ob['id'],'executionPlanId':pc['execution_plan_id']})
 db.sql('workspace',"UPDATE workspace.plan_changes SET status='applying',execution_operation_id="+q(execution)+' WHERE id='+q(pc['id'])+'; UPDATE workspace.subscription_period_obligations SET resource_execution_started_at='+q(clock)+' WHERE id='+q(ob['id'])+';')
 db.insert('fabric','resource_actions',id='boundary-resize:'+pc['id'],resource_set_id=ctx['set'],command_id='boundary-command:'+pc['id'],action='resize',provider_idempotency_key='boundary-resize:'+pc['id'],approved_input={'executionPlanId':pc['execution_plan_id'],'executionPlanDigest':pc['execution_plan_digest']},authorization_receipt_ref='bounded-fixture-plan-authority',observation_result='confirmed',evidence_ref='confirmed-boundary-resource:'+pc['id'],execution_epoch=3)
 snapshot={'periodStart':pc['next_period_start'],'periodEnd':pc['next_period_end'],'currentMonthlyUSDMicros':str(pc['target_monthly_usd_micros']),'quoteId':ob['quote_id'],'refundPolicyVersionId':'test-refund','retentionPolicyVersionId':'test-retention','refundTerms':'accepted next-period base policy','retentionTerms':'accepted target retention'}
 period={'id':'next-period:'+pc['id'],'subscription_id':ctx['subscription'],'quote_id':ob['quote_id'],'accepted_quote_snapshot':snapshot,'period_start':pc['next_period_start'],'period_end':pc['next_period_end'],'billing_key':ob['billing_key'],'charge_wallet_operation_id':ob['wallet_operation_id'],'charge_receipt_id':'confirmed-next-period-receipt:'+pc['id'],'provenance':'quoted'}
 sql='BEGIN; SELECT id FROM workspace.workspaces WHERE id='+q(ctx['workspace'])+' FOR UPDATE; '+sql_insert('workspace.subscription_periods',period)+' UPDATE workspace.subscriptions SET current_period_start='+q(pc['next_period_start'])+',current_period_end='+q(pc['next_period_end'])+',current_price_policy_version_id='+q(pc['target_price_policy_version_id'])+',current_monthly_usd_micros='+str(pc['target_monthly_usd_micros'])+',version=version+1,provenance=\'quoted\',accepted_quote_id='+q(ob['quote_id'])+',accepted_quote_snapshot='+j(snapshot)+',legacy_purchase_id=NULL,legacy_obligation_snapshot=NULL WHERE id='+q(ctx['subscription'])+' AND version='+str(sub['version'])+'; UPDATE workspace.workspaces SET status=\'active\',compute_plan_id='+q(pc['target_compute_plan_id'])+',storage_plan_id='+q(pc['target_storage_plan_id'])+' WHERE id='+q(ctx['workspace'])+'; UPDATE workspace.plan_changes SET status=\'applied\',applied_at='+q(clock)+',actual_outcome='+j({'deliveryOutcome':'applied','resourceOutcome':'target_confirmed','currentRequirementValidation':'valid'})+' WHERE id='+q(pc['id'])+'; COMMIT;';db.sql('workspace',sql)
 return {'state':'applied','providerEffects':1,'executionOperationId':execution,'initialOperationId':pc['operation_id'],'sourceVersionBridge':ob['confirmed_subscription_version'],'newMonthlyUSDMicros':str(pc['target_monthly_usd_micros'])}


def extended_plan_case(db,pb,case):
 ctx=seed_plan_source(db,case);mutation=case['mutation']
 if mutation=='future_other':
  E=case['periodEnd'];db.insert('workspace','subscription_period_obligations',id='other-future:'+case['id'],subscription_id=ctx['subscription'],workspace_id=ctx['workspace'],period_start=E,period_end=next_billing_month(E,int(E[8:10])),target_compute_plan_id='test-compute',target_storage_plan_id='test-storage',target_price_policy_version_id='test-price',amount_usd_micros=20000000,accepted_pricing_snapshot={'source':'already accepted other next period'},status='accepted',billing_key='other-future-key:'+case['id'],payment_accepted_at=case['quoteAt'],quote_id='other-accepted-quote:'+case['id'])
  return make_plan(db,pb,case,ctx)
 pc,calc=make_plan(db,pb,case,ctx,force_kind='downgrade_next_period')
 if mutation=='worker_early':
  result=execute_scheduled_change(db,ctx,pc,case['quoteAt']);assert result['state']=='scheduled' and result['providerEffects']==0;assert db.sql('gateway','SELECT count(*) FROM gateway.wallet_operations WHERE workspace_id='+q(ctx['workspace'])+';')=='0';return result
 if mutation=='next_period_failure':
  ob=next_period_obligation(db,ctx,pc,accept=True,clock=pc['next_period_start'])
  failure='failed-boundary-execution:'+pc['id'];db.insert('workspace','operations',id=failure,tenant_id='test-tenant',actor_id='test-actor',kind='execute_plan_change',resource_id=pc['id'],status='failed',stage='failed',request_id='failed-boundary:'+pc['id'],accepted_input={'periodObligationId':ob['id']},error_code='SCHEDULED_PLAN_APPLICATION_CONFLICT',completed_at=pc['next_period_start'])
  db.sql('workspace',"UPDATE workspace.plan_changes SET status='failed',execution_operation_id="+q(failure)+',actual_outcome='+j({'deliveryOutcome':'failed','resourceOutcome':'unchanged','targetActivated':False,'fenceReceiptId':'confirmed-original-execution-fence','failureReceiptId':'confirmed-unsupported-next-period'})+' WHERE id='+q(pc['id'])+"; UPDATE workspace.workspaces SET status='suspended' WHERE id="+q(ctx['workspace'])+';')
  refund_original(db,ctx,pc,ob['wallet_operation_id'],10000000,'next_period_plan_failure_full','next-period-failure:'+pc['id']);assert pc['charge_usd_micros']==0
  return {'currentPeriodSupplement':'0','actualNextPeriodOriginalPayment':'10000000','nextPeriodRefund':'10000000','oldUnpaidEntitlementExtended':False,'initialScheduledOperationNotReopened':True}
 if mutation in ['own_prepay','wrong_version_after_prepay','runtime_risk']:
  ob=next_period_obligation(db,ctx,pc,accept=True,clock=case['quoteAt'])
  if mutation=='wrong_version_after_prepay':db.sql('workspace','UPDATE workspace.subscriptions SET version=version+1 WHERE id='+q(ctx['subscription'])+';')
  if mutation=='runtime_risk':
   before=db.one_json('workspace','SELECT version FROM workspace.subscriptions WHERE id='+q(ctx['subscription']))
   db.sql('workspace','UPDATE workspace.workspaces SET model_configuration_version=model_configuration_version+1,version=version+1 WHERE id='+q(ctx['workspace'])+'; UPDATE workspace.plan_changes SET actual_outcome='+j({'deliveryOutcome':'pending','resourceOutcome':'unchanged','currentRequirementValidation':'at_risk','riskCode':'TARGET_RUNTIME_REQUIREMENTS_CHANGED'})+' WHERE id='+q(pc['id'])+';')
   assert db.one_json('workspace','SELECT version FROM workspace.subscriptions WHERE id='+q(ctx['subscription']))==before
  result=execute_scheduled_change(db,ctx,pc,pc['next_period_start']);assert result['state']=='applied' and result['executionOperationId']!=result['initialOperationId'];return result
 if mutation=='cancel_rebind_unpaid':
  ob=next_period_obligation(db,ctx,pc,accept=False);cancel_plan(db,ctx,pc);newquote='new-explicit-renew-quote:'+case['id'];oldkey=ob['billing_key'];oldid=ob['id']
  # A new explicit Quote replaces only an unfunded current intent; old Quote/PlanChange remain immutable history.
  changed=db.sql('workspace','UPDATE workspace.subscription_period_obligations SET plan_change_id=NULL,target_compute_plan_id='+q(pc['source_compute_plan_id'])+',target_storage_plan_id='+q(pc['source_storage_plan_id'])+',target_price_policy_version_id='+q(pc['source_price_policy_version_id'])+',amount_usd_micros='+str(pc['source_monthly_usd_micros'])+',quote_id='+q(newquote)+',accepted_pricing_snapshot='+j({'quoteId':newquote,'explicitReplacementOf':ob['quote_id'],'amountUSDMicros':str(pc['source_monthly_usd_micros'])})+',version=version+1 WHERE id='+q(oldid)+' AND version='+str(ob['version'])+' AND payment_accepted_at IS NULL AND wallet_operation_id IS NULL AND resource_execution_started_at IS NULL RETURNING id;')
  owner_assert(bool(changed),'funded original period must never be repriced')
  after=db.one_json('workspace','SELECT id,billing_key,amount_usd_micros FROM workspace.subscription_period_obligations WHERE id='+q(oldid));assert after['id']==oldid and after['billing_key']==oldkey and after['amount_usd_micros']==20000000
  return {'obligationIdentityUnchanged':True,'billingKeyUnchanged':True,'newExplicitPriceUSDMicros':'20000000','oldPlanAndQuoteRetained':True}
 raise AssertionError('unknown extended plan scenario')


def enum_type_mapping_audit():
 """Inspect direct API->column mappings for semantic type and enum mismatches.
 Derived mappings have explicit owner rules and are exercised by scenario adapters.
 """
 tables={t['owner']+'.'+t['name']:t for t in INVENTORY['tables']};columns={key+'.'+f['name']:(t,f) for key,t in tables.items() for f in t['fields']};checked=0;issues=[]
 for name,schema in SCHEMAS.items():
  for prop,source in schema.get('x-field-map',{}).items():
   if not isinstance(source,str):continue
   if source not in columns:
    issues.append(f'{name}.{prop}: missing direct source {source}');continue
   t,f=columns[source];wire=schema['properties'][prop]
   ref=wire.get('$ref','');refname=ref.rsplit('/',1)[-1]
   if ref:wire={**SCHEMAS[refname],**{k:v for k,v in wire.items() if k!='$ref'}}
   typ=wire.get('type');dbtyp=f['type'];allowed={
    'string':{'text','timestamptz'},'integer':{'integer','bigint'},'boolean':{'boolean'},'object':{'jsonb'},'array':{'jsonb','text[]'}
   }.get(typ,set())
   if refname in ['USDMicros','NonnegativeInt64']:allowed={'bigint'}
   if allowed and dbtyp not in allowed:issues.append(f'{name}.{prop}: {typ}/{refname} cannot map directly to {source}:{dbtyp}')
   if 'enum' in wire and typ=='string':
    pattern=r"CHECK \("+re.escape(f['name'])+r" IN \((.*?)\)\)"
    found=[re.search(pattern,c) for c in t['constraints']];enums=[m.group(1) for m in found if m]
    if enums:
     dbvalues=set(re.findall(r"'([^']*)'",enums[0]));wirevalues=set(wire['enum'])
     if dbvalues!=wirevalues:issues.append(f'{name}.{prop} enum mismatch {wirevalues} vs {source} {dbvalues}')
   checked+=1
 if issues:raise RuntimeError('Direct semantic field mapping mismatches:\n'+'\n'.join(issues))
 return {'directMappingsTypeAndEnumChecked':checked,'optionalWireFieldMeansOmittedNotNull':True,'nonnullableDbMayBeInternalOrDerived':'not inferred from API required alone; legacy cases cover real discriminators'}

HANDLERS={'plan_change_extended':extended_plan_case,'plan_change':plan_change_case,'billing_anchor':billing_anchor_case,'legacy_descriptor':legacy_descriptor_case,'renewal_consent':renewal_consent_case,'quote':quote_case,'publisher':publisher_case,'reference':reference_case,'authorization':authorization_case,'authorization_platform':authorization_platform_case,'authorization_grant':authorization_grant_case,'route':route_case,'tenant_lifecycle':tenant_lifecycle_case}

def main():
 ap=argparse.ArgumentParser(description=__doc__);ap.add_argument('--output',type=Path);ap.add_argument('--case-prefix',default='',help='Run an explicit ID prefix; receipt marks this subset');ap.add_argument('--image',default='sha256:3c5c8892d184f738f4fe282d14ddaa613a38f00f4189d2d94725ebe6f2909ddb',help='Exact already-present PostgreSQL16 image ID; no pull or tag substitution');args=ap.parse_args()
 sources={p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest() for p in SOURCE_FILES}
 now=datetime.now(timezone.utc);stamp=now.strftime('%Y%m%dT%H%M%S.%fZ');output=args.output or ROOT/'checks/runs'/('cross-domain-'+stamp+'.json')
 if output.exists():raise RuntimeError('refusing to overwrite prior evidence receipt: '+str(output))
 name='opl-spec-semantic-'+str(int(time.time()))
 results=[];setup_error=None;cleanup={'containerAbsent':False};db=Database(name);metadata={}
 image=json.loads(subprocess.check_output(['docker','image','inspect',args.image,'--format','{{json .}}'],text=True))
 subprocess.run(['docker','run','--rm','-d','--network','none','--name',name,'--tmpfs','/var/lib/postgresql/data','-e','POSTGRES_HOST_AUTH_METHOD=trust',args.image],check=True,capture_output=True,text=True)
 try:
  for _ in range(100):
   p=subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','postgres'],capture_output=True,text=True)
   if p.returncode==0:break
   time.sleep(.1)
  else:raise RuntimeError('isolated PostgreSQL not ready')
  db.setup();metadata['actualDatabaseFields']=db.verify_inventory();metadata.update(enum_type_mapping_audit());metadata['postgresVersion']=db.sql('postgres','SELECT version();')
  seed_catalog(db);seed_publishers(db)
  with tempfile.TemporaryDirectory(prefix='opl-semantic-proto-') as td:
   pb=protocol_module(Path(td))
   for case in [c for c in CASES if c['id'].startswith(args.case_prefix)]:
    entry={'id':case['id'],'suite':case['suite'],'expected':case['expected']}
    try:
     details=HANDLERS[case['suite']](db,pb,case);entry.update(actual='accept',details=details,passed=case['expected']=='accept')
    except Rejection as e:entry.update(actual=e.layer,reason=str(e),passed=e.layer==case['expected'])
    except PgError as e:entry.update(actual='unexpected_database_error',sqlState=e.state,reason=str(e),passed=False)
    except Exception as e:entry.update(actual='unexpected_error',reason=type(e).__name__+': '+str(e),passed=False)
    results.append(entry);print(('PASS' if entry['passed'] else 'FAIL'),entry['id'],entry['actual'],entry.get('reason','') if not entry['passed'] else '',flush=True)
 except Exception as e:setup_error=type(e).__name__+': '+str(e)
 finally:
  stop=subprocess.run(['docker','stop',name],capture_output=True,text=True)
  absent=False;polls=0;deadline=time.monotonic()+10
  while time.monotonic()<deadline:
   inspect=subprocess.run(['docker','container','inspect',name],capture_output=True,text=True);polls+=1
   if inspect.returncode!=0 and 'No such container' in inspect.stderr:
    absent=True;break
   time.sleep(.1)
  cleanup={'containerName':name,'containerAbsent':absent,'stopSucceeded':stop.returncode==0,'removalReadbackPolls':polls,'storage':'tmpfs only; no host mounts/named volumes','network':'none; no host ports'}
 end_sources={p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest() for p in SOURCE_FILES};unchanged=sources==end_sources
 passed=setup_error is None and bool(results) and all(x['passed'] for x in results) and cleanup['containerAbsent'] and unchanged
 receipt={'schemaVersion':'opl-cross-domain-execution/v1','executedAt':now.isoformat(),'completedAt':datetime.now(timezone.utc).isoformat(),'passed':passed,'sourceHashes':sources,'sourceUnchangedDuringRun':unchanged,'sourceHashesAtEnd':end_sources,'image':{'name':args.image,'repoTags':image.get('RepoTags',[]),'id':image['Id'],'repoDigests':image.get('RepoDigests',[])},'metadata':metadata,'setupError':setup_error,'cases':results,'caseSelection':{'prefix':args.case_prefix,'fullSuite':args.case_prefix==''},'sqlInvocations':db.sql_count,'cleanup':cleanup,'scope':'typed REST/protobuf specification adapters -> isolated PostgreSQL -> exact typed/readback projection; provider router and Storage are explicitly named fixtures, not implemented services','notProven':['real service handlers/Saga workers','production or Instance adoption','real Gateway/provider charges or resources','actual implementation/provider/Instance acceptance remain separate from approved D17 specification']}
 output.parent.mkdir(parents=True,exist_ok=True);output.write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n');print('RECEIPT',output);print('RESULT',passed,'CASES',len(results),'SETUP_ERROR',setup_error)
 return 0 if passed else 1
if __name__=='__main__':raise SystemExit(main())

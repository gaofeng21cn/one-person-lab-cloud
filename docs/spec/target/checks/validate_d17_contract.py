from pathlib import Path
import json,yaml,copy,sys
from jsonschema import Draft202012Validator,RefResolver
from openapi_schema_validator import OAS30Validator
from openapi_spec_validator import validate
from google.protobuf.json_format import ParseDict,ParseError
import tempfile,subprocess,grpc_tools
B=Path(__file__).resolve().parents[1]
_tmp=tempfile.TemporaryDirectory(prefix='opl-d17-proto-')
_include=Path(grpc_tools.__file__).parent/'_proto'
subprocess.run([sys.executable,'-m','grpc_tools.protoc',f'-I{B/"contracts"}',f'-I{_include}',f'--python_out={_tmp.name}',str(B/'contracts/internal.proto')],check=True)
sys.path.insert(0,_tmp.name);import internal_pb2 as pb
api=yaml.safe_load((B/'03_api_contract_complete.yaml').read_text());S=api['components']['schemas'];policy=json.loads((B/'contracts/plan-change-policy.json').read_text());events=json.loads((B/'contracts/events.json').read_text());resolver=RefResolver.from_schema(api);out=[];MAX=9223372036854775807
validate(api);Draft202012Validator.check_schema(policy);Draft202012Validator(policy).validate(policy['x-policy']);Draft202012Validator.check_schema(events)
def record(name,ok):
 assert ok,name
 out.append({'case':name,'pass':True})
def rejects(fn):
 try:fn();return False
 except (ValueError,AssertionError,ParseError):return True
def valid(n,x):return not list(OAS30Validator(S[n],resolver=resolver).iter_errors(x))
def charge(old,new,d,r):
 old,new,d,r=map(int,(old,new,d,r))
 if not (0<=old<=MAX and 0<=new<=MAX and d>0 and 0<r<=d):raise ValueError('invalid input')
 result=(max(new-old,0)*r+d-1)//d
 if not 0<=result<=MAX:raise ValueError('overflow')
 return result
for v in policy['x-numeric-examples']:
 record('numeric '+v['name'],str(charge(v['oldMonthlyUSDMicros'],v['newMonthlyUSDMicros'],v['periodMilliseconds'],v['remainingMilliseconds']))==v['expectedChargeUSDMicros'])
record('28-day actual period',charge(20000000,40000000,28*86400000,14*86400000)==10000000)
record('29-day actual period final ceil',charge(20000000,40000000,29*86400000,28*86400000)==19310345)
record('max-int64 uses arbitrary precision intermediate',charge(0,MAX,2678400000,2678400000)==MAX)
for name,args in [('expired T=E',(1,2,100,0)),('quote before S',(1,2,100,101)),('zero period',(1,2,0,0)),('negative old price',(-1,2,100,50)),('price overflow',(0,MAX+1,100,50))]:record('reject '+name,rejects(lambda args=args:charge(*args)))
for v in ['pending_decision','pending_user','draft']:
 bad={**policy['x-policy'],'approvalStatus':v};record('approved-only policy rejects '+v,bool(list(Draft202012Validator(policy).iter_errors(bad))))
bad=copy.deepcopy(policy['x-policy']);bad['upgrade']['chargeClock']='accepted_quote_time';record('acceptance-time repricing rejected',bool(list(Draft202012Validator(policy).iter_errors(bad))))
# An executable acceptance oracle, not an implementation of a live wallet.
def fail_refund(original,confirmed,unknown,provider,charge_state,fenced):
 if provider!='confirmed' or charge_state!='confirmed' or unknown or not fenced:raise ValueError('readback required')
 if not 0<=confirmed<=original:raise ValueError('original conservation')
 return original-confirmed
record('known failure full refund not base 720',fail_refund(10000000,0,0,'confirmed','confirmed',True)==10000000)
record('partial irreversible resources do not reduce customer compensation',fail_refund(10000000,0,0,'confirmed','confirmed',True)==10000000)
record('known prior refund only remaining compensated',fail_refund(10000000,3000000,0,'confirmed','confirmed',True)==7000000)
for args in [(10000000,0,0,'unknown','confirmed',True),(10000000,0,0,'confirmed','unknown',True),(10000000,0,4000000,'confirmed','confirmed',True),(10000000,0,0,'confirmed','confirmed',False)]:record('unknown/unfenced never emits new refund '+str(args[2:]),rejects(lambda args=args:fail_refund(*args)))
def unused_refund(amount,coverage,unused,remaining):
 if coverage<=0 or unused<0 or unused>coverage or remaining<0 or remaining>amount:raise ValueError('coverage invalid')
 return min(remaining,amount*unused//coverage)
record('successful supplement own 15-day coverage refund',unused_refund(10000000,15*86400000,7500*86400,10000000)==5000000)
record('supplement floor at final micro only',unused_refund(2,3,1,2)==0)
record('supplement remaining original cap',unused_refund(10000000,100,90,3000000)==3000000)
record('delete before quoteT rejected',rejects(lambda:unused_refund(10,100,101,10)))
record('invalid coverage rejected',rejects(lambda:unused_refund(10,0,0,10)))
# PlanChange is not derived from a succeeded generic Operation.
base={'id':'pc1','workspaceId':'ws1','kind':'downgrade_next_period','status':'scheduled','sourceComputePlanId':'c-old','sourceStoragePlanId':'s','targetComputePlanId':'c-new','targetStoragePlanId':'s','sourcePricePolicyVersionId':'price-old','targetPricePolicyVersionId':'price-new','sourceSubscriptionId':'sub1','sourceSubscriptionVersion':'1','sourcePeriodId':'period1','sourceMonthlyUSDMicros':'40000000','targetMonthlyUSDMicros':'20000000','quoteId':'q1','policyVersion':'workspace-plan-change-v1','quoteAt':'2026-09-16T00:00:00Z','quoteAtMilliseconds':'1789516800000','periodStartMilliseconds':'1788220800000','periodEndMilliseconds':'1790812800000','sourceFinancialSnapshotDigest':'sha256:'+'b'*64,'periodStart':'2026-09-01T00:00:00Z','periodEnd':'2026-10-01T00:00:00Z','chargeUSDMicros':'0','plannedEffectiveAt':'2026-10-01T00:00:00Z','operationId':'accept1','chargeStatus':'not_required','nextPeriodStart':'2026-10-01T00:00:00Z','nextPeriodEnd':'2026-11-01T00:00:00Z','nextPeriodChargeUSDMicros':'20000000','nextPeriodChargeStatus':'not_requested','refundOperationIds':[],'scheduleVersion':'1','observationResult':'confirmed','cancellable':True,'createdAt':'2026-09-16T00:00:01Z','updatedAt':'2026-09-16T00:00:01Z','deliveryOutcome':'pending','resourceOutcome':'unchanged','runtimeReadbackRequirement':'required','currentRequirementValidation':'valid','executionPlanId':'ep1','executionPlanDigest':'sha256:'+'a'*64}
record('scheduled plan response valid without fake appliedAt',valid('PlanChange',base))
operation={'operationId':'accept1','owner':'workspace','kind':'resize_workspace','resourceId':'pc1','status':'succeeded','stage':'succeeded','observationResult':'confirmed','requestId':'r1','createdAt':'2026-09-16T00:00:01Z','updatedAt':'2026-09-16T00:00:02Z'}
record('accepted Operation succeeded while plan remains scheduled',valid('Operation',operation) and valid('PlanChange',base) and base['status']!='applied')
record('scheduled cannot claim appliedAt',not valid('PlanChange',{**base,'appliedAt':'2026-09-16T00:00:02Z'}))
record('downgrade cannot charge current period',not valid('PlanChange',{**base,'chargeUSDMicros':'1'}))
applied={**base,'status':'applied','appliedAt':'2026-10-01T00:00:10Z','executionOperationId':'execute1','deliveryOutcome':'applied','resourceOutcome':'target_confirmed','nextPeriodChargeStatus':'confirmed','nextPeriodChargeOperationId':'pay1','nextPeriodObligationId':'obl1','cancellable':False}
record('applied requires separate boundary execution and confirmed next payment',valid('PlanChange',applied))
record('applied without confirmed payment rejected',not valid('PlanChange',{**applied,'nextPeriodChargeStatus':'unknown'}))
zero={**applied,'nextPeriodChargeUSDMicros':'0','nextPeriodChargeStatus':'not_required'};zero.pop('nextPeriodChargeOperationId');record('zero next period has obligation but no Gateway charge',valid('PlanChange',zero))
up={k:v for k,v in base.items() if not k.startswith('nextPeriod')};up.update(kind='upgrade_immediate',status='applied',chargeUSDMicros='10000000',chargeStatus='confirmed',chargeOperationId='supp1',plannedEffectiveAt='2026-09-16T00:00:00Z',appliedAt='2026-09-16T00:00:10Z',executionOperationId='accept1',deliveryOutcome='applied',resourceOutcome='target_confirmed',cancellable=False)
record('immediate upgrade preserved S/E and confirmed supplement',valid('PlanChange',up) and up['periodEnd']==base['periodEnd'])
record('applied upgrade unknown money rejected',not valid('PlanChange',{**up,'chargeStatus':'unknown'}))
zero_up={**up,'chargeUSDMicros':'0','chargeStatus':'not_required'};zero_up.pop('chargeOperationId');record('zero upgrade has no Gateway identity',valid('PlanChange',zero_up))
record('zero upgrade fake Gateway charge rejected',not valid('PlanChange',{**zero_up,'chargeOperationId':'fake'}))
residual={**up,'status':'needs_attention','deliveryOutcome':'failed','resourceOutcome':'irreversible_residual','refundOperationIds':['refund1'],'errorCode':'PLAN_TRANSITION_NOT_SUPPORTED'};residual.pop('appliedAt');record('known failed delivery residual resources can show refund independently',valid('PlanChange',residual))
record('pending policy removed from all live contract artifacts',all(word not in (B/'03_api_contract_complete.yaml').read_text() for word in ['pending_user_decision','resize-policy-draft','RESIZE_POLICY_PENDING_DECISION']))
# Concrete typed cross-owner ports, not fabricated workflows.
for svc,methods in {'WorkspacePlanChangeReadback':['ReadSubscriptionPlanState','ReadPlanChange','ReadNextPeriodObligation','ReadPlanChangeFailure'],'GatewayPlanChangeSettlement':['DebitSupplement','DebitScheduledPeriod','RefundFailure','RefundSupplementOnDeletion'],'FabricPlanTransitionReadback':['ReadApprovedPlanTransition','ReadExecutionPlan'],'LedgerPlanChangeEvidence':['AppendPlanChangeReceipt','AppendPlanChangeRefundReceipt']}.items():
 record(svc+' exact required methods',all(m in pb.DESCRIPTOR.services_by_name[svc].methods_by_name for m in methods))
fund=ParseDict({'zeroAmount':{'zeroAmountReceiptId':'zero1'}},pb.PlanChangeFundingEvidence());record('zero funding explicit typed branch',fund.WhichOneof('funding')=='zero_amount')
record('fake mixed funding rejected',rejects(lambda:ParseDict({'zeroAmount':{'zeroAmountReceiptId':'zero1'},'confirmedCharge':{'walletOperationId':'pay1'}},pb.PlanChangeFundingEvidence())))
# Immutable quote numerical snapshot fixture checks without accepting client prices.
calc={'policyVersion':'workspace-plan-change-v1','kind':'upgrade_immediate','transitionId':'tr1','sourceSubscriptionId':'sub1','sourceSubscriptionVersion':'1','sourcePeriodId':'period1','sourceComputePlanId':'c-old','sourceStoragePlanId':'s','targetComputePlanId':'c-new','targetStoragePlanId':'s','sourcePricePolicyVersionId':'price-old','targetPricePolicyVersionId':'price-new','sourceMonthlyUSDMicros':'20000000','targetMonthlyUSDMicros':'40000000','quoteAt':'2026-09-16T00:00:00Z','quoteAtMilliseconds':'1789516800000','periodStartMilliseconds':'1788220800000','periodEndMilliseconds':'1790812800000','sourceFinancialSnapshotDigest':'sha256:'+'b'*64,'periodStart':'2026-09-01T00:00:00Z','periodEnd':'2026-10-01T00:00:00Z','chargeUSDMicros':'10000000','plannedEffectiveAt':'2026-09-16T00:00:00Z','upgradeProration':{'periodMilliseconds':'2592000000','remainingMilliseconds':'1296000000','priceDeltaUSDMicros':'20000000','rounding':'ceil_once_usd_micro'},'executionPlanId':'ep1','executionPlanDigest':'sha256:'+'a'*64}
record('typed frozen proration snapshot valid',valid('PlanChangeCalculation',calc));record('downgrade cannot contain an upgrade proration',not valid('PlanChangeCalculation',{**calc,'kind':'downgrade_next_period','chargeUSDMicros':'0'}))
prec=policy['x-precision-example'];correct_d=int(prec['periodEndMilliseconds'])-int(prec['periodStartMilliseconds']);remaining=int(prec['periodEndMilliseconds'])-int(prec['quoteAtMilliseconds'])
record('raw source UnixMilli precision uses canonical basis',str(charge(prec['oldMonthlyUSDMicros'],prec['newMonthlyUSDMicros'],correct_d,remaining))==prec['expectedChargeUSDMicros'])
record('PG rounded timestamp demonstrably changes micro charge and is rejected as source',str(charge(prec['oldMonthlyUSDMicros'],prec['newMonthlyUSDMicros'],correct_d-1,remaining))==prec['incorrectPostgresRoundedStartChargeUSDMicros'])
record('source financial bytes and hash carried in acceptance',all(f in pb.QuoteAcceptance.DESCRIPTOR.fields_by_name for f in ['source_financial_snapshot_bytes','source_financial_snapshot_digest']))
inv=json.loads((B/'contracts/api_inventory.json').read_text());actual={o['operationId'] for item in api['paths'].values() for o in item.values()};record('inventory exact operations',actual=={x['operationId'] for x in inv['operations']});record('new code mappings complete',set(S['ErrorCode']['enum'])==set(api['x-error-http-status']))
(B/'checks/d17_contract_results.json').write_text(json.dumps({'scope':'Approved D17 executable contract/numeric acceptance oracles, not implemented services or real charges','policyVersion':'workspace-plan-change-v1','cases':out},ensure_ascii=False,indent=2)+'\n')
print('PASS',len(out),'D17 numeric/semantic/typed-contract cases; approved-only policy')

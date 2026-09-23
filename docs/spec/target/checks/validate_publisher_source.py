#!/usr/bin/env python3
"""Validate draft Publisher examples with the actual Cloud canonical Go validator, read-only."""
from pathlib import Path
from datetime import datetime,timezone
import argparse,hashlib,json,os,subprocess,tempfile
parser=argparse.ArgumentParser();parser.add_argument('--cloud-repo',required=True);args=parser.parse_args()
root=Path(__file__).resolve().parents[1];repo=Path(args.cloud_repo).resolve();module=repo/'packages/contracts/go'
if not (module/'go.mod').is_file():raise SystemExit('Expected actual Cloud contracts Go module')
main=(root/'checks/publisher_source_check.go').read_text()
main=main.replace('os.ReadFile("/Users/huangrende/Desktop/应聘/chatgpt-share-6ab13ad3/opl_v226_execution_spec/contracts/publisher-contract.schema.json")','os.ReadFile(os.Args[1])')
with tempfile.TemporaryDirectory(prefix='opl-publisher-source-') as temp:
 work=Path(temp);(work/'main.go').write_text(main)
 (work/'go.mod').write_text('module opl-publisher-source-check\n\ngo 1.22\n\nrequire opl-cloud/packages/contracts/go v0.0.0\nreplace opl-cloud/packages/contracts/go => '+str(module)+'\n')
 result=subprocess.run(['go','run','.',str(root/'contracts/publisher-contract.schema.json')],cwd=work,env={**os.environ,'GOWORK':'off'},capture_output=True,text=True)
 source_hashes={str(f.relative_to(repo)):hashlib.sha256(f.read_bytes()).hexdigest() for f in sorted(module.glob('workspace_application*.go')) if not f.name.endswith('_test.go')}
 report={'schemaVersion':1,'timestamp':datetime.now(timezone.utc).isoformat(),'passed':result.returncode==0,'scope':'Publisher sample contract accepted by current canonical Go decoder/validator/startup DAG; not a container build or deployment','sourceSHA':subprocess.check_output(['git','-C',str(repo),'rev-parse','HEAD'],text=True).strip(),'sourceHashes':source_hashes,'publisherSchemaHash':hashlib.sha256((root/'contracts/publisher-contract.schema.json').read_bytes()).hexdigest(),'stdout':result.stdout,'stderr':result.stderr,'command':'GOWORK=off go run <isolated-check-module> <publisher-schema>','productSourceChanged':False}
 path=root/'checks/runs'/('publisher-source-'+datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')+'.json');path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps({'passed':report['passed'],'receipt':str(path),'output':result.stdout,'error':result.stderr},ensure_ascii=False,indent=2))
 raise SystemExit(0 if report['passed'] else 1)

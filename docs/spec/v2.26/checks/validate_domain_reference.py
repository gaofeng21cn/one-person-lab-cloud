#!/usr/bin/env python3
"""Validate the derived domain inventory and expose unresolved cross-owner contracts.

This is a read-only contract check, not proof of an implemented service or DDD
certification. --allow-known-gaps only permits inspection/report generation; the
report still declares the gaps and is rejected by the handoff join.
"""
from pathlib import Path
from datetime import datetime, timezone
import argparse
import hashlib
import json
import subprocess
import sys
import tempfile
import grpc_tools
import yaml
from google.protobuf.descriptor_pb2 import FileDescriptorSet

ROOT = Path(__file__).resolve().parents[1]

def contract_gaps(messages, events, api):
    """Check real declared identities, not invented fixture domain models."""
    gaps = []
    operation = {f.name for f in messages['OwnerOperationRequest'].field}
    if 'owner' not in operation:
        gaps.append({'id': 'ALIGN-01', 'owner': 'shared-contract / each operation owner',
                     'reason': 'OwnerOperationRequest drops the owner required by owner+operationId routing'})
    branches = events['oneOf']
    mappings = set()
    identity_complete = bool(branches)
    for b in branches:
        event_type = b['properties']['eventType']['const']
        version = b['properties']['schemaVersion']['const']
        identity = b.get('x-aggregate-identity', {})
        payload = events['$defs'][b['properties']['payload']['$ref'].split('/')[-1]]
        pair = (event_type, version)
        if pair in mappings or not identity.get('type') or identity.get('idPayloadField') not in payload.get('required', []):
            identity_complete = False
        mappings.add(pair)
    wire = any(f.name == 'aggregate_type' for f in messages['EventEnvelope'].field)
    explicit = wire and all('aggregateType' in b['properties'] and 'aggregateType' in b.get('required', []) for b in branches)
    derived = (identity_complete and not wire and
               'x-aggregate-identity.type' in events.get('x-delivery', {}).get('outboxFieldMap', {}).get('aggregateType', '') and
               'x-aggregate-identity.idPayloadField' in events.get('x-delivery', {}).get('outboxFieldMap', {}).get('aggregateId', ''))
    if not (explicit or derived):
        gaps.append({'id': 'ALIGN-02', 'owner': 'event producer contracts',
                     'reason': 'each exact event version needs a unique aggregate type and required payload ID mapping (or an explicitly validated wire type); no inferred prefix'})
    delivery = {f.name for f in messages['DeliverEventRequest'].field}
    dual = any({'tenant', 'gateway'} <= set(b['x-consumers']) for b in branches)
    if dual and 'consumer_owner' not in delivery:
        gaps.append({'id': 'ALIGN-03', 'owner': 'shared-delivery-contract / consumer owner',
                     'reason': 'tenant and gateway subscribe at one endpoint but DeliverEventRequest does not identify the destination owner'})
    generic = api['paths']['/api/v2/operations/{owner}/{operationId}']['get']
    admin = api['paths']['/api/v2/admin/operations']['get']
    reconcile = api['paths']['/api/v2/admin/operations/{owner}/{operationId}/reconcile']['post']
    expected_targets=((generic,'operation-path-owner'),(admin,'required-owner-query'),(reconcile,'operation-path-owner'))
    if any(op.get('x-owner') != 'bff' or op.get('x-target-owner') != target or op.get('x-tables') != ['{owner}.operations'] for op,target in expected_targets):
        gaps.append({'id': 'ALIGN-04', 'owner': 'BFF routing / API metadata',
                     'reason': 'generic Operation REST entrypoint/target owner mapping is not explicit and consistent'})
    return gaps


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--allow-known-gaps', action='store_true')
    args = p.parse_args()
    api = yaml.safe_load((ROOT/'03_api_contract_complete.yaml').read_text())
    inv = json.loads((ROOT/'contracts/api_inventory.json').read_text())['operations']
    tables = json.loads((ROOT/'contracts/db_inventory.json').read_text())['tables']
    events = json.loads((ROOT/'contracts/events.json').read_text())
    with tempfile.TemporaryDirectory(prefix='opl-domain-descriptor-') as temp:
        output = Path(temp)/'spec.pb'
        subprocess.run([sys.executable, '-m', 'grpc_tools.protoc', '-I'+str(ROOT/'contracts'),
                        '-I'+str(Path(grpc_tools.__file__).parent/'_proto'),
                        '--descriptor_set_out='+str(output), '--include_imports',
                        str(ROOT/'contracts/internal.proto')], check=True, capture_output=True)
        descriptor = FileDescriptorSet()
        descriptor.ParseFromString(output.read_bytes())
        proto = next(f for f in descriptor.file if f.name == 'internal.proto')
    messages = {m.name: m for m in proto.message_type}
    errors = []
    for owner in {t['owner'] for t in tables}:
        doc = (ROOT/f'reference/domain-{owner}.md').read_text()
        for op in (x for x in inv if x['owner'] == owner):
            if f'### {op["operationId"]}\n' not in doc:
                errors.append('missing REST operation: '+op['operationId'])
        for t in (x for x in tables if x['owner'] == owner):
            heading = f'### {t["schema"]}.{t["name"]}\n'
            if heading not in doc:
                errors.append('missing table '+heading); continue
            section = doc.split(heading, 1)[1].split('\n### ', 1)[0]
            for field in t['fields']:
                if '| `'+field['name']+'` |' not in section:
                    errors.append('missing DB field: '+t['name']+'.'+field['name'])
    bff_doc = (ROOT/'reference/rest-schemas.md').read_text()
    for op in (x for x in inv if x['owner'] == 'bff'):
        if f'### {op["operationId"]}\n' not in bff_doc:
            errors.append('missing BFF route: '+op['operationId'])
    rpc_doc = (ROOT/'reference/rpc-messages.md').read_text()
    for m in proto.message_type:
        heading = '## '+m.name+'\n'
        if heading not in rpc_doc:
            errors.append('missing proto message: '+m.name); continue
        section = rpc_doc.split(heading, 1)[1].split('\n## ', 1)[0]
        for f in m.field:
            if f'{f.name} #{f.number}' not in section:
                errors.append('missing proto field: '+m.name+'.'+f.name)
    schemas_doc = (ROOT/'reference/rest-schemas.md').read_text()
    for name, schema in api['components']['schemas'].items():
        heading = '## '+name+'\n'
        if heading not in schemas_doc:
            errors.append('missing REST schema: '+name); continue
        section = schemas_doc.split(heading, 1)[1].split('\n## ', 1)[0]
        for field in schema.get('properties', {}):
            if '| `'+field+'` |' not in section:
                errors.append('missing REST field: '+name+'.'+field)
    gaps = contract_gaps(messages, events, api)
    now = datetime.now(timezone.utc)
    paths = ['03_api_contract_complete.yaml', 'contracts/internal.proto', 'contracts/events.json',
             'contracts/db_inventory.json', 'contracts/api_inventory.json', 'contracts/domain_flows.json',
             'checks/validate_domain_reference.py', 'checks/render_domain_reference.py', '15_domain_alignment.md']
    paths += [str(f.relative_to(ROOT)) for f in sorted((ROOT/'reference').glob('*.md'))]
    report = {'schemaVersion': 1, 'timestamp': now.isoformat(),
              'scope': 'derived API/DB/message field coverage plus explicit cross-owner identity gaps; not runtime qualification',
              'referenceCoveragePassed': not errors, 'passed': not errors and not gaps,
              'errors': errors, 'openContractFindings': gaps,
              'counts': {'restOperations': len(inv), 'tables': len(tables),
                         'dbColumns': sum(len(t['fields']) for t in tables),
                         'rpcMethods': sum(len(s.method) for s in proto.service),
                         'messages': len(messages), 'eventTypes': len(events['oneOf'])},
              'sourceHashes': {path: hashlib.sha256((ROOT/path).read_bytes()).hexdigest() for path in paths}}
    output = ROOT/'checks/runs'/('domain-alignment-'+now.strftime('%Y%m%dT%H%M%S%fZ')+'.json')
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    print(json.dumps({k: v for k, v in report.items() if k != 'sourceHashes'}, ensure_ascii=False, indent=2))
    print('RECEIPT', output)
    return 1 if errors or (gaps and not args.allow_known_gaps) else 0

if __name__ == '__main__':
    raise SystemExit(main())

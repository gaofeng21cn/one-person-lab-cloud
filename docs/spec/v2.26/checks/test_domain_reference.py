"""Check gap detection against actual v2.26 definitions, without a parallel DTO."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import grpc_tools
import yaml
from google.protobuf.descriptor_pb2 import FileDescriptorSet, FieldDescriptorProto
from validate_domain_reference import contract_gaps

ROOT = Path(__file__).resolve().parents[1]

class DomainReferenceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)/'schema.pb'
            subprocess.run([sys.executable, '-m', 'grpc_tools.protoc', '-I'+str(ROOT/'contracts'),
                            '-I'+str(Path(grpc_tools.__file__).parent/'_proto'),
                            '--descriptor_set_out='+str(output), '--include_imports',
                            str(ROOT/'contracts/internal.proto')], check=True, capture_output=True)
            descriptor = FileDescriptorSet()
            descriptor.ParseFromString(output.read_bytes())
            cls.proto = next(f for f in descriptor.file if f.name == 'internal.proto')
        cls.events = json.loads((ROOT/'contracts/events.json').read_text())
        cls.api = yaml.safe_load((ROOT/'03_api_contract_complete.yaml').read_text())

    def inputs(self):
        p = copy.deepcopy(self.proto)
        return {m.name: m for m in p.message_type}, copy.deepcopy(self.events), copy.deepcopy(self.api)

    def ids(self, inputs):
        return {g['id'] for g in contract_gaps(*inputs)}

    def test_current_authoritative_contract_has_no_identity_gap(self):
        self.assertEqual(self.ids(self.inputs()), set())

    def test_missing_owner_is_detected(self):
        m, e, a = self.inputs()
        m['OwnerOperationRequest'].ClearField('field')
        self.assertIn('ALIGN-01', self.ids((m, e, a)))

    def test_wire_field_without_explicit_validation_is_not_alignment(self):
        m, e, a = self.inputs()
        m['EventEnvelope'].field.add(name='aggregate_type', number=11, type=FieldDescriptorProto.TYPE_STRING)
        self.assertIn('ALIGN-02', self.ids((m, e, a)))

    def test_missing_exact_aggregate_id_source_is_detected(self):
        m, e, a = self.inputs()
        branch = next(b for b in e['oneOf'] if b['properties']['eventType']['const'] == 'fabric.route_observed.v1')
        e['$defs']['RouteObserved']['required'].remove('routeBindingId')
        self.assertIn('ALIGN-02', self.ids((m, e, a)))

    def test_shared_endpoint_needs_destination_owner(self):
        m, e, a = self.inputs()
        self.assertTrue(any({'tenant', 'gateway'} <= set(b['x-consumers']) for b in e['oneOf']))
        m['DeliverEventRequest'].ClearField('field')
        self.assertIn('ALIGN-03', self.ids((m, e, a)))

    def test_generic_operation_metadata_is_not_a_workspace_table(self):
        m, e, a = self.inputs()
        op = a['paths']['/api/v2/operations/{owner}/{operationId}']['get']
        self.assertEqual(op['x-tables'], ['{owner}.operations'])
        self.assertNotIn('ALIGN-04', self.ids((m, e, a)))
        op['x-owner'] = 'workspace'
        self.assertIn('ALIGN-04', self.ids((m, e, a)))

if __name__ == '__main__':
    unittest.main()

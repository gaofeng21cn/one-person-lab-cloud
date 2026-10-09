"""Exercise the real plan validator with current plan inputs in an isolated checkout."""
from pathlib import Path
import json
import shutil
import subprocess
import sys
import tempfile
import unittest

SPEC = Path(__file__).resolve().parents[1]
CLOUD = SPEC.parents[2]


class RepositoryPlacementTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='opl-plan-scope-')
        self.addCleanup(self.temp.cleanup)
        self.cloud = Path(self.temp.name) / 'opl-cloud'
        self.spec = self.cloud / 'docs/spec/target'
        self.spec.mkdir(parents=True)
        # Use the real inventories and task schema, never a second domain model.
        for relative in [
            '00_master_index.md', '01_domain_ownership_matrix.md',
            '14_implementation_work_packages.md', '03_api_contract_complete.yaml',
            '12_product_spec.md', 'checks/development_plan.json',
            'checks/render_development_plan.py', 'checks/validate_development_plan.py',
            'contracts/api_inventory.json', 'contracts/db_inventory.json',
            'contracts/ui_inventory.json', 'contracts/internal.proto',
        ]:
            target = self.spec / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(SPEC / relative, target)
        (self.spec / 'checks/runs').mkdir()
        original = json.loads((SPEC / 'checks/development_plan.json').read_text())
        self.plan = json.loads(json.dumps(original).replace(str(CLOUD), str(self.cloud)))
        # The validator requires existing sources inside the containing checkout.
        # Materialize their existence in the fixture instead of granting an
        # absolute read path to the real repository.
        for work, source in zip(self.plan['workPackages'], original['workPackages']):
            work['existingReadPaths'] = list(source['existingReadPaths'])
            for path in source['existingReadPaths']:
                existing = CLOUD / path
                target = self.cloud / path
                if existing.is_dir():
                    target.mkdir(parents=True, exist_ok=True)
                else:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(existing, target)
        self.plan['sourceRoots']['instance'] = str(Path(self.temp.name) / 'opl-instance-medopl')
        old_instance = original['sourceRoots']['instance']
        for work in self.plan['workPackages']:
            work['plannedWritePaths'] = [
                path.replace(old_instance, self.plan['sourceRoots']['instance'])
                for path in work['plannedWritePaths']
            ]
        for item in self.plan['executionSlices']:
            item['writePaths'] = [
                path.replace(old_instance, self.plan['sourceRoots']['instance'])
                for path in item['writePaths']
            ]
            for path in item.get('testWritePaths', []):
                target = self.cloud / path
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(CLOUD / path, target)

    def validate(self):
        (self.spec / 'checks/development_plan.json').write_text(json.dumps(self.plan))
        result = subprocess.run(
            [sys.executable, str(self.spec / 'checks/validate_development_plan.py')],
            text=True, capture_output=True,
        )
        self.assertIn(result.returncode, (0, 1), result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(result.returncode == 0, report['passed'])
        return report

    def test_current_plan_accepts_external_instance_owner(self):
        self.assertTrue(self.validate()['passed'])

    def test_cloud_domain_cannot_be_a_sibling_repository(self):
        self.plan['sourceRoots']['capability'] = str(self.cloud.parent / 'other-repository')
        self.assertIn('Cloud owner escapes repository: capability', self.validate()['errors'])

    def test_tenant_cannot_be_split_into_another_module(self):
        self.plan['sourceRoots']['tenant'] = str(self.cloud / 'services/tenant')
        self.assertIn('CloudIdentity must share the Gateway Integration module', self.validate()['errors'])

    def test_cloud_contract_task_cannot_write_instance(self):
        work = next(w for w in self.plan['workPackages'] if w['id'] == 'W01')
        work['plannedWritePaths'].append(self.plan['sourceRoots']['instance'] + '/receipts/new.json')
        self.assertTrue(any('W01 write escapes authorized repository scope' in e for e in self.validate()['errors']))

    def test_dot_dot_cannot_escape_cloud_scope(self):
        work = next(w for w in self.plan['workPackages'] if w['id'] == 'W01')
        work['plannedWritePaths'].append(str(self.cloud / '../outside/file'))
        self.assertTrue(any('W01 write escapes authorized repository scope' in e for e in self.validate()['errors']))

    def test_default_app_cannot_require_an_agent_build(self):
        target = next(s for s in self.plan['executionSlices'] if s['id'] == 'W29.default-tke')
        target['startAfter'].append('W09.agent-build')
        self.assertIn('default App wrongly depends on a synthetic Agent Build', self.validate()['errors'])

    def test_default_app_cannot_require_optional_webui(self):
        target = next(s for s in self.plan['executionSlices'] if s['id'] == 'W29.default-tke')
        target['startAfter'].append('W07.webui-catalog')
        self.assertIn('default App wrongly depends on optional Package or catalog UI', self.validate()['errors'])

    def test_first_create_requires_the_real_serve_producer(self):
        target = next(s for s in self.plan['executionSlices'] if s['id'] == 'W15.first-create')
        target['startAfter'].remove('W17.first-delivery')
        self.assertIn('first-create lacks a real identity/payment/delivery/evidence producer', self.validate()['errors'])

    def test_slice_cycle_is_rejected(self):
        target = next(s for s in self.plan['executionSlices'] if s['id'] == 'W01.application-contracts')
        target['startAfter'].append('W15.first-create')
        self.assertTrue(any('cyclic slice dependencies' in e for e in self.validate()['errors']))

    def test_receipt_unknown_recovery_must_be_defined(self):
        target = next(s for s in self.plan['executionSlices'] if s['id'] == 'W05.receipts')
        target['onUnknown'] = ''
        self.assertIn('slice omitted onUnknown: W05.receipts', self.validate()['errors'])

    def test_cloud_slice_cannot_write_instance(self):
        target = next(s for s in self.plan['executionSlices'] if s['id'] == 'W01.application-contracts')
        target['writePaths'].append(self.plan['sourceRoots']['instance'] + '/deploy/new.json')
        self.assertIn('slice write escapes owner boundary: W01.application-contracts', self.validate()['errors'])


    def test_preparation_cannot_bypass_shared_contract_gate(self):
        self.plan['parallelPreparation'][0]['sharedContractGate'] = ''
        self.assertIn('preparation bypasses contract gate: artifacts', self.validate()['errors'])

    def test_preparation_cannot_edit_another_domain(self):
        self.plan['parallelPreparation'][0]['writePaths'].append('services/serve')
        self.assertIn('preparation write overlap: delivery / artifacts', self.validate()['errors'])

    def test_preparation_cannot_edit_shared_contract(self):
        self.plan['parallelPreparation'][0]['writePaths'].append('packages/contracts/proto')
        self.assertIn('preparation writes shared boundary: artifacts', self.validate()['errors'])

    def test_preparation_model_is_user_selected(self):
        self.plan['parallelPreparation'][0]['model'] = 'other'
        self.assertIn('dispatch model differs: artifacts', self.validate()['errors'])


    def test_serial_successor_keeps_user_selected_model(self):
        self.plan['serialIntegration']['reasoningEffort'] = 'low'
        self.assertIn('serial integration model differs', self.validate()['errors'])

    def test_serial_successor_cannot_invent_a_work_package(self):
        self.plan['serialIntegration']['stages'][0]['workPackages'].append('W99')
        self.assertIn('serial stage invented work package', self.validate()['errors'])


class ApprovedSelectionTests(unittest.TestCase):
    """Validate the adopted target union, not a fabricated parallel DTO."""
    @classmethod
    def setUpClass(cls):
        import yaml
        from jsonschema import Draft7Validator
        from referencing import Registry, Resource
        from referencing.jsonschema import DRAFT7
        api = yaml.safe_load((SPEC / '03_api_contract_complete.yaml').read_text())
        uri = 'urn:opl:approved-api-target'
        registry = Registry().with_resource(uri, Resource.from_contents(api, default_specification=DRAFT7))
        cls.application = Draft7Validator({'$ref': uri + '#/x-approved-wire-migration/targetSelections/WorkspaceApplicationSelection'}, registry=registry)
        cls.build = Draft7Validator({'$ref': uri + '#/components/schemas/CreateBuildRequest'}, registry=registry)

    def test_application_branches_are_explicit_and_exclusive(self):
        for value in [{'kind': 'opl_app', 'runtimeVersionId': 'runtime_1'}, {'kind': 'agent', 'capabilityVersionId': 'cap_1'}]:
            self.assertTrue(self.application.is_valid(value), value)
        for value in [{}, {'kind': 'opl_app'}, {'kind': 'opl_app', 'capabilityVersionId': 'cap_1'}, {'kind': 'agent', 'runtimeVersionId': 'runtime_1', 'capabilityVersionId': 'cap_1'}]:
            self.assertFalse(self.application.is_valid(value), value)

    def test_agent_build_requires_package_runtime_and_independent_webui(self):
        valid = {'packageVersionId': 'pkg_1', 'runtimeVersionId': 'runtime_1', 'webuiVersionId': 'ui_1'}
        self.assertTrue(self.build.is_valid(valid))
        for missing in valid:
            self.assertFalse(self.build.is_valid({k: v for k, v in valid.items() if k != missing}))
        self.assertFalse(self.build.is_valid({**valid, 'webuiSelection': {'kind': 'runtime_builtin'}}))


if __name__ == '__main__':
    unittest.main()

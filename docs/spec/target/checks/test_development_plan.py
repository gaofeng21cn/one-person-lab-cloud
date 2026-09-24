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
            '14_implementation_work_packages.md', 'checks/development_plan.json',
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
        # Existing source entries are read-only provenance for this validator.
        # Keep them at the real checkout; only planned writes and roots move.
        for work, source in zip(self.plan['workPackages'], original['workPackages']):
            # Existing-source provenance stays anchored to the real checkout; planned
            # writes are resolved against the isolated temporary checkout by the validator.
            work['existingReadPaths'] = [
                str((CLOUD / path).resolve()) if not Path(path).is_absolute() else path
                for path in source['existingReadPaths']
            ]
        self.plan['sourceRoots']['instance'] = str(Path(self.temp.name) / 'opl-instance-medopl')
        old_instance = original['sourceRoots']['instance']
        for work in self.plan['workPackages']:
            work['plannedWritePaths'] = [
                path.replace(old_instance, self.plan['sourceRoots']['instance'])
                for path in work['plannedWritePaths']
            ]

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


if __name__ == '__main__':
    unittest.main()

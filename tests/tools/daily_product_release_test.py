import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('daily_cloud_release', ROOT / 'tools/daily-product-release.py')
daily = importlib.util.module_from_spec(spec)
spec.loader.exec_module(daily)


class DailyCloudRelease(unittest.TestCase):
    def test_unknown_dispatch_reconciles_the_same_run_without_another_post(self):
        row = {'id': 123, 'head_branch': 'main', 'display_title': 'fixture frozen source',
               'status': 'completed', 'conclusion': 'success', 'html_url': 'https://example.invalid/123'}
        calls = []
        def api(path, *, instance=False, payload=None):
            calls.append((path, payload))
            if payload is not None:
                raise RuntimeError('synthetic dispatch response lost')
            if len(calls) == 1:
                return {'workflow_runs': []}
            return {'workflow_runs': [row]}
        with patch.object(daily, 'api', api), patch.object(daily.time, 'sleep'):
            self.assertEqual(daily.ensure_run('fixture.yml', {'product_sha': 'a' * 40}, row['display_title'])['id'], 123)
        self.assertEqual(sum(payload is not None for _, payload in calls), 1)

    def test_tag_entry_reuses_in_progress_main_continuation(self):
        row = {'id': 123, 'head_branch': 'main', 'display_title': 'Cloud product v0.1.9',
               'status': 'in_progress'}
        with patch.object(daily, 'api', return_value={'workflow_runs': [row]}) as api:
            daily.dispatch_tag('v0.1.9')
            self.assertEqual(api.call_count, 1)


if __name__ == '__main__': unittest.main()

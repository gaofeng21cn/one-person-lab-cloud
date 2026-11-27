import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('daily_cloud_release', ROOT / 'tools/daily-product-release.py')
daily = importlib.util.module_from_spec(spec)
spec.loader.exec_module(daily)


class DailyCloudRelease(unittest.TestCase):
    def test_blocked_continuation_keeps_only_verified_frozen_identity(self):
        tag = 'v0.1.21'
        source = 'a' * 40
        outputs = [source, '{"version":"0.1.21"}', '', '']
        with patch.dict(daily.os.environ, {'RELEASE_TAG': tag}), \
                patch.object(daily, 'command', side_effect=outputs), \
                patch.object(daily, 'ensure_run', side_effect=RuntimeError('candidate failed')):
            receipt = daily.continuation_receipt(ROOT)
        self.assertEqual(receipt, {'schema_version': 'opl.cloud-daily-release.v1',
                                  'instance_deployment_dispatched': False, 'status': 'blocked',
                                  'reason': 'candidate failed', 'tag': tag, 'product_sha': source})

        outputs = [source, '{"version":"0.1.21"}', '', RuntimeError('tag source is not on main')]
        with patch.dict(daily.os.environ, {'RELEASE_TAG': tag}), \
                patch.object(daily, 'command', side_effect=outputs), \
                patch.object(daily, 'ensure_run') as ensure_run:
            receipt = daily.continuation_receipt(ROOT)
        ensure_run.assert_not_called()
        self.assertEqual(receipt, {'schema_version': 'opl.cloud-daily-release.v1',
                                  'instance_deployment_dispatched': False, 'status': 'blocked',
                                  'reason': 'tag source is not on main'})

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

    def test_unknown_native_retry_resumes_the_same_failed_run_once(self):
        old = {'id': 456, 'head_branch': 'main', 'display_title': 'fixed source',
               'status': 'completed', 'conclusion': 'failure', 'run_attempt': 1,
               'html_url': 'https://example.invalid/456'}
        observed = {**old, 'status': 'in_progress', 'conclusion': None, 'run_attempt': 2}
        completed = {**observed, 'status': 'completed', 'conclusion': 'success'}
        posts = []
        reads = [observed, completed]
        def api(path, *, instance=False, payload=None):
            if 'workflow_dispatch' in path: return {'workflow_runs': [old]}
            if payload is not None:
                posts.append(path)
                raise RuntimeError('synthetic lost retry response')
            return reads.pop(0)
        with patch.object(daily, 'api', api), patch.object(daily, 'command') as command:
            result = daily.ensure_run('fixture.yml', {}, 'fixed source')
        self.assertEqual((result['id'], result['run_attempt'], result['conclusion']), (456, 2, 'success'))
        self.assertEqual(posts, [f'repos/{daily.REPO}/actions/runs/456/rerun-failed-jobs'])
        self.assertEqual(command.call_count, 1)

    def test_completed_continuation_uses_updated_main_orchestration(self):
        row = {'id': 123, 'head_branch': 'main', 'head_sha': 'a' * 40,
               'display_title': 'Cloud product v0.1.9', 'status': 'completed'}
        calls = []
        def api(path, *, instance=False, payload=None):
            calls.append((path, payload))
            if 'commits/main' in path: return {'sha': 'b' * 40}
            return {'workflow_runs': [row]}
        with patch.object(daily, 'api', api): daily.dispatch_tag('v0.1.9')
        posts = [(path, payload) for path, payload in calls if payload is not None]
        self.assertEqual(posts, [(f'repos/{daily.REPO}/actions/workflows/daily-release.yml/dispatches',
                                 {'ref': 'main', 'inputs': {'release_tag': 'v0.1.9'}})])

    def test_tag_entry_reuses_in_progress_main_continuation(self):
        row = {'id': 123, 'head_branch': 'main', 'display_title': 'Cloud product v0.1.9',
               'status': 'in_progress'}
        with patch.object(daily, 'api', return_value={'workflow_runs': [row]}) as api:
            daily.dispatch_tag('v0.1.9')
            self.assertEqual(api.call_count, 1)


if __name__ == '__main__': unittest.main()

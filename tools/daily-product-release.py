#!/usr/bin/env python3
"""Continue native Cloud publication; never dispatch an Instance deployment."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

REPO = 'gaofeng21cn/one-person-lab-cloud'
INSTANCE = 'gaofeng21cn/opl-instance-medopl'


def command(args, *, instance=False, data=None, timeout=300):
    env = dict(os.environ)
    if instance:
        env['GH_TOKEN'] = env['OPL_INSTANCE_EVIDENCE_TOKEN']
    result = subprocess.run(args, input=data, text=True, capture_output=True, env=env, timeout=timeout)
    if result.returncode:
        raise RuntimeError('Cloud release operation failed: ' + ' '.join(args[:3]) + '\n' + result.stderr[-1200:])
    return result.stdout


def api(path, *, instance=False, payload=None):
    args = ['gh', 'api', path]
    if payload is not None:
        args += ['--method', 'POST', '--input', '-']
    text = command(args, instance=instance, data=json.dumps(payload) if payload is not None else None)
    return json.loads(text) if text.strip() else None


def ensure_run(workflow, inputs, title):
    def matches():
        runs = api(f'repos/{REPO}/actions/workflows/{workflow}/runs?event=workflow_dispatch&per_page=100')['workflow_runs']
        return sorted([r for r in runs if r['display_title'] == title and r['head_branch'] == 'main'], key=lambda row: row['id'], reverse=True)
    runs = matches()
    if not runs:
        # Reconcile an ambiguous dispatch by its frozen input/title before retrying.
        try:
            api(f'repos/{REPO}/actions/workflows/{workflow}/dispatches', payload={'ref': 'main', 'inputs': inputs})
        except RuntimeError:
            for _ in range(12):
                runs = matches()
                if runs: break
                time.sleep(5)
            if not runs: raise
        for _ in range(12):
            runs = matches()
            if runs: break
            time.sleep(5)
    if not runs:
        raise RuntimeError('Dispatched Cloud operation is not yet reconciled')
    run = runs[0]
    if run['status'] == 'completed' and run['conclusion'] != 'success':
        raise RuntimeError('Existing Cloud operation requires repair: ' + run['html_url'])
    if run['status'] != 'completed':
        command(['gh', 'run', 'watch', str(run['id']), '--repo', REPO, '--exit-status', '--compact', '--interval', '10'], timeout=7200)
        run = api(f"repos/{REPO}/actions/runs/{run['id']}")
    if run['conclusion'] != 'success':
        raise RuntimeError('Cloud operation did not succeed')
    return run


def download(run, name, destination, *, instance=False):
    destination.mkdir(parents=True, exist_ok=True)
    command(['gh', 'run', 'download', str(run), '--repo', INSTANCE if instance else REPO,
             '--name', name, '--dir', str(destination)], instance=instance)


def installation_run(candidate, receipt_digest, root):
    runs = api(f'repos/{INSTANCE}/actions/workflows/deployment-verify.yml/runs?status=success&per_page=100', instance=True)['workflow_runs']
    for run in runs:
        if run['event'] != 'workflow_dispatch' or run['head_branch'] != 'main':
            continue
        artifacts = api(f"repos/{INSTANCE}/actions/runs/{run['id']}/artifacts?per_page=100", instance=True)['artifacts']
        name = 'opl-cloud-deployment-verification-' + str(run['id'])
        if not any(row['name'] == name and not row['expired'] for row in artifacts):
            continue
        directory = root / str(run['id'])
        download(run['id'], name, directory, instance=True)
        for file in directory.rglob('*.json'):
            value = json.loads(file.read_text())
            if (value.get('status') == 'deployment_verified'
                and value.get('productSha') == candidate['product']['sha']
                and value.get('productTree') == candidate['product']['tree']
                and value.get('candidateReceiptDigest') == receipt_digest
                and value.get('cloudImageDigest') == candidate['cloudImage']['indexDigest']):
                return run['id']
    return None


def execute(root):
    tag = os.environ['RELEASE_TAG']
    if not re.fullmatch(r'v0\.[0-9]+\.[0-9]+', tag):
        raise RuntimeError('Invalid frozen Cloud product tag')
    source = command(['git', 'rev-parse', tag+'^{commit}']).strip()
    version = json.loads(command(['git', 'show', tag+':product-version.json']))['version']
    if not re.fullmatch(r'[a-f0-9]{40}', source) or tag != 'v' + version:
        raise RuntimeError('Cloud tag does not select the product version')
    command(['git', 'fetch', 'origin', 'main', '--quiet'])
    command(['git', 'merge-base', '--is-ancestor', source, 'origin/main'])
    candidate_run = ensure_run('build-opl-cloud-candidate.yml', {'product_sha': source, 'release_tag': tag}, 'Cloud candidate ' + source)
    candidate_directory = root / 'candidate'
    download(candidate_run['id'], f"opl-cloud-candidate-{source}-{candidate_run['run_attempt']}", candidate_directory)
    command(['node', 'tools/cloud-candidate-receipt.ts', 'validate-bundle', '--bundle', str(candidate_directory)])
    receipt = candidate_directory / 'opl-cloud-candidate.json'
    candidate = json.loads(receipt.read_text())
    if candidate['product']['sha'] != source:
        raise RuntimeError('Candidate selects different product bytes')
    receipt_digest = command(['node', 'tools/cloud-candidate-receipt.ts', 'digest', '--receipt', str(receipt)]).strip()
    local = ensure_run('clean-host-qualification.yml', {'ref': source, 'cloud_image_ref': candidate['cloudImage']['indexRef'], 'mode': 'local-v2'}, 'Cloud local qualification ' + source)
    result = {'schema_version': 'opl.cloud-daily-release.v1', 'product_sha': source, 'tag': tag,
              'candidate_run_id': candidate_run['id'], 'candidate_receipt_digest': receipt_digest,
              'local_qualification_run_id': local['id'], 'instance_deployment_dispatched': False}
    instance = installation_run(candidate, receipt_digest, root / 'instance')
    if instance is None:
        return {**result, 'status': 'qualification_pending', 'required_evidence': 'Same-Candidate Instance deployment-verify receipt. Reuse the retained Candidate; production deployment remains Instance-owned.'}
    pulls = api(f'repos/{REPO}/commits/{source}/pulls')
    merged = [p for p in pulls if p.get('merged_at') and p.get('merge_commit_sha') == source]
    heads = {p['head']['sha'] for p in merged} | {source}
    runs = api(f'repos/{REPO}/actions/workflows/pull-request-ci.yml/runs?event=pull_request&status=success&per_page=100')['workflow_runs']
    checked = [r for r in runs if r['head_sha'] in heads]
    if not checked:
        raise RuntimeError('Exact source or identical merged PR tree has no successful complete source CI')
    source_run = max(checked, key=lambda row: row['id'])
    publication = ensure_run('release-opl-cloud-image.yml', {
        'version': tag, 'ref': source, 'candidate_run_id': str(candidate_run['id']),
        'local_qualification_run_id': str(local['id']), 'instance_deployment_verification_run_id': str(instance),
        'source_verification_run_id': str(source_run['id']), 'automation_source_run_id': os.environ['GITHUB_RUN_ID']},
        'Cloud release ' + tag + ' / ' + source)
    return {**result, 'status': 'published', 'publication_run_id': publication['id'], 'instance_qualification_run_id': instance,
            'source_verification_run_id': source_run['id']}


def dispatch_tag(tag):
    if not re.fullmatch(r'v0\.[0-9]+\.[0-9]+', tag):
        raise RuntimeError('Invalid product tag')
    runs = api(f'repos/{REPO}/actions/workflows/daily-release.yml/runs?event=workflow_dispatch&per_page=100')['workflow_runs']
    selected = sorted([row for row in runs if row['head_branch'] == 'main' and row['display_title'] == 'Cloud product '+tag], key=lambda row:row['id'], reverse=True)
    if selected:
        run = selected[0]
        if run['status'] == 'completed':
            api(f"repos/{REPO}/actions/runs/{run['id']}/rerun", payload={})
        return
    api(f'repos/{REPO}/actions/workflows/daily-release.yml/dispatches', payload={'ref':'main','inputs':{'release_tag':tag}})


if __name__ == '__main__' and len(sys.argv) == 3 and sys.argv[1] == '--dispatch-tag':
    dispatch_tag(sys.argv[2])
elif __name__ == '__main__':
    output = Path(os.environ['RUNNER_TEMP']) / 'cloud-daily-release-result.json'
    try:
        with tempfile.TemporaryDirectory(prefix='cloud-daily-') as temporary:
            result = execute(Path(temporary))
    except Exception as error:
        result = {'schema_version': 'opl.cloud-daily-release.v1', 'status': 'blocked', 'reason': str(error), 'instance_deployment_dispatched': False}
    output.write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result, indent=2))
    if os.getenv('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as stream:
            stream.write('Cloud product pipeline: `' + result['status'] + '`.\n')
    raise SystemExit(1 if result['status'] == 'blocked' else 0)

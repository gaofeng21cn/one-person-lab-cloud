# Adapted replay operation; not a historical receipt. See provenance.json.
from pathlib import Path
import subprocess,os,json,datetime,hashlib
p=Path('__RUN_ROOT__');env=os.environ.copy()
for key in list(env):
 if key.startswith('OPL_'):env.pop(key)
values={'OPL_RUNTIME_IMAGE':'uswccr.ccs.tencentyun.com/oplcloud/one-person-lab-app@sha256:8efab2844b52c66bcaa3da05637abc3cbdb0c3b29364718839b121284711de92','OPL_BUILD_TEST_PLATFORM':'linux/amd64','OPL_BUILD_TEST_PACKAGE':str(p/'inputs/ibd-evidence-assistant-v2-0.2.0-upload-package.zip'),'OPL_BUILD_TEST_RUNTIME_CONTRACT':str(p/'inputs/runtime-publisher-contract.json'),'OPL_BUILD_TEST_RUNTIME_LOCAL':'1','OPL_BUILD_TEST_HANDOFF':str(p/'evidence/build-handoff.json'),'OPL_BUILD_TEST_HANDOFF_ONLY':'1','OPL_CAPABILITY_PACKAGE_SCHEMA_PATH':str(p/'inputs/agent-plugins-manifest.schema.json'),'OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST':'sha256:0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883'}
env.update(values)
cmd=[str(p/'bin/build.test'),'-test.run=^TestLivePackageBuildAndRestartReadback$','-test.count=1','-test.v','-test.timeout=12m']
rec={'startedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sourceSha':'3cefc17ea9e065a880039f416721522512ea5215','command':cmd,'environment':values,'boundaries':['isolated external identity fixture','local Runtime and WebUI catalog admission','new disposable loopback Registry','no production or Candidate qualification']}
(p/'evidence/build-invocation-retry-1.json').write_text(json.dumps(rec,indent=2)+'\n')
with (p/'evidence/build-owner-retry-1.log').open('w') as log:run=subprocess.run(cmd,cwd=p/'source/services/build/internal/build',env=env,stdout=log,stderr=subprocess.STDOUT)
text=(p/'evidence/build-owner-retry-1.log').read_text();rec.update({'completedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'exitCode':run.returncode,'passed':run.returncode==0 and '--- PASS: TestLivePackageBuildAndRestartReadback' in text and '--- SKIP' not in text,'logSha256':'sha256:'+hashlib.sha256(text.encode()).hexdigest()})
(p/'evidence/build-result-retry-1.json').write_text(json.dumps(rec,indent=2)+'\n');print(text[-12000:],flush=True);raise SystemExit(run.returncode)

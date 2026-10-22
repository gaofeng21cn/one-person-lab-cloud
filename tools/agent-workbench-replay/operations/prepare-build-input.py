# Adapted replay operation; not a historical receipt. See provenance.json.
from pathlib import Path
import subprocess,json,urllib.request,tarfile,hashlib,datetime,time
p=Path('__RUN_ROOT__');repo='opl-dev/'+p.name+'/webui';host='__DEV_REGISTRY__'
base='http://'+host+'/v2/'+repo
with tarfile.open(p/'inputs/agent-webui.oci.tar') as t:
 idx=json.load(t.extractfile('index.json'));d=idx['manifests'][0]['digest'];assert d=='sha256:88790a22db669daa917ccf6a2f1d7650235ebd70f810696ce237e4fc39edcb69'
 def blob(d):
  b=t.extractfile('blobs/sha256/'+d.split(':')[1]).read();assert 'sha256:'+hashlib.sha256(b).hexdigest()==d;return b
 manifest=blob(d);m=json.loads(manifest)
 for desc in [m['config'],*m['layers']]:
  body=blob(desc['digest']);assert len(body)==desc['size']
  req=urllib.request.Request(base+'/blobs/uploads/',data=b'',method='POST')
  with urllib.request.urlopen(req) as res:location=res.headers['Location']
  sep='&' if '?' in location else '?'
  with urllib.request.urlopen(urllib.request.Request(location+sep+'digest='+desc['digest'],data=body,method='PUT',headers={'Content-Type':'application/octet-stream'})) as res:assert res.status==201
 req=urllib.request.Request(base+'/manifests/source',data=manifest,method='PUT',headers={'Content-Type':m['mediaType']})
 with urllib.request.urlopen(req) as res:assert res.headers['Docker-Content-Digest']==d
 req=urllib.request.Request(base+'/manifests/'+d,headers={'Accept':m['mediaType']})
 with urllib.request.urlopen(req) as res:actual=res.read();assert actual==manifest
 ref=host+'/'+repo+'@'+d
 record={'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'image':ref,'digest':d,'exactManifestReadback':True,'configDigest':m['config']['digest'],'layers':[x['digest'] for x in m['layers']]}
 (p/'evidence/webui-registry.receipt.json').write_text(json.dumps(record,indent=2)+'\n')
 # Only the harness WebUI build input changes. FROM pins the previously exported static artifact.
 target=p/'source/webui/cloud-agent/Dockerfile';target.write_text('FROM '+ref+'\n')
 (p/'evidence/webui-fixture-input.json').write_text(json.dumps({'baselineSha':'3cefc17ea9e065a880039f416721522512ea5215','path':'webui/cloud-agent/Dockerfile','content':target.read_text(),'reason':'select the immutable extracted WebUI as the disposable harness build input','productGoSourceChanged':False},indent=2)+'\n')
 print(json.dumps(record),flush=True)
# Tooling tags in the existing harness; reuse the retained PostgreSQL bytes.
subprocess.run(['docker','tag','sha256:301bcb60b8a3ee4ab7e147932723e3abd1cef53516ce5210b39fd9fe5e3602ae','postgres:16'],check=True)
for image in ['moby/buildkit:buildx-stable-1','docker/dockerfile:1']:
 print('TOOL_IMAGE',image,flush=True)
 assert json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]['Id']==json.loads((p/'inputs/tooling.json').read_text())['images'][image]['imageId']
print('TOOLS_READY',flush=True)

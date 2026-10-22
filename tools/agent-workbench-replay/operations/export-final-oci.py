# Adapted replay operation; not a historical receipt. See provenance.json.
from pathlib import Path
import subprocess,urllib.request,json,tarfile,hashlib,shutil,zipfile,datetime
p=Path('__RUN_ROOT__');h=json.loads((p/'evidence/build-handoff.json').read_text());a=h['capabilityVersion']['artifact'];repo=a['repository'];digest=a['digest'];base='http://'+repo.split('/',1)[0]+'/v2/'+repo.split('/',1)[1];out=p/'artifacts';out.mkdir(exist_ok=True);stage=out/'oci-layout';(stage/'blobs/sha256').mkdir(parents=True,exist_ok=True)
accept='application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json'
def fetch(d,kind,size=None):
 req=urllib.request.Request(base+'/'+kind+'/'+d,headers={'Accept':accept});target=stage/'blobs/sha256'/d.split(':')[1];hasher=hashlib.sha256();length=0
 with urllib.request.urlopen(req) as res,target.open('wb') as f:
  while True:
   chunk=res.read(1<<20)
   if not chunk:break
   f.write(chunk);hasher.update(chunk);length+=len(chunk)
 assert 'sha256:'+hasher.hexdigest()==d
 if size is not None:assert length==size
 return target
manifest=fetch(digest,'manifests');m=json.loads(manifest.read_text());fetch(m['config']['digest'],'blobs',m['config']['size'])
for layer in m['layers']:fetch(layer['digest'],'blobs',layer['size'])
(stage/'oci-layout').write_text('{"imageLayoutVersion":"1.0.0"}')
index={'schemaVersion':2,'mediaType':'application/vnd.oci.image.index.v1+json','manifests':[{'mediaType':m['mediaType'],'digest':digest,'size':manifest.stat().st_size,'platform':{'os':'linux','architecture':'amd64'}}]};(stage/'index.json').write_text(json.dumps(index,separators=(',',':')))
expected=json.loads((p/'inputs/original-webui-oci.projection.json').read_text())['staticFiles'];web={};carrier={}
with zipfile.ZipFile(p/'inputs/ibd-evidence-assistant-v2-0.2.0-upload-package.zip') as z:
 packageExpected={n[len('carrier/'):]:hashlib.sha256(z.read(n)).hexdigest() for n in z.namelist() if n.startswith('carrier/') and not n.endswith('/')}
for layer in m['layers']:
 with tarfile.open(stage/'blobs/sha256'/layer['digest'].split(':')[1],mode='r:*') as t:
  for member in t:
   name=member.name.lstrip('./')
   if not member.isfile():continue
   if name.startswith('web/') and name[4:] in expected:web[name[4:]]='sha256:'+hashlib.sha256(t.extractfile(member).read()).hexdigest()
   if name.startswith('opt/ibd-carrier/') and name[len('opt/ibd-carrier/'):] in packageExpected:carrier[name[len('opt/ibd-carrier/'):]]=hashlib.sha256(t.extractfile(member).read()).hexdigest()
assert web==expected,'final image WebUI bytes do not match exported input'
assert carrier==packageExpected,'final image Package bytes do not match upload'
archive=out/'application.oci.tar'
with tarfile.open(archive,'w') as t:
 for name in ['oci-layout','index.json','blobs']:t.add(stage/name,arcname=name)
hasher=hashlib.sha256()
with archive.open('rb') as f:
 for chunk in iter(lambda:f.read(1<<20),b''):hasher.update(chunk)
record={'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'finalImage':repo+'@'+digest,'ociManifestDigest':digest,'archive':str(archive),'archiveBytes':archive.stat().st_size,'archiveSha256':'sha256:'+hasher.hexdigest(),'registeredWebuiDigest':h['buildArtifact']['input']['webuiArtifact']['digest'],'originalWebuiManifestDigest':'sha256:88790a22db669daa917ccf6a2f1d7650235ebd70f810696ce237e4fc39edcb69','finalWebuiFilesMatchExtractedArtifact':True,'verifiedWebuiFiles':len(web),'finalCarrierFilesMatchUploadedPackage':True,'verifiedCarrierFiles':len(carrier),'configDigest':m['config']['digest'],'layerDigests':[x['digest'] for x in m['layers']]}
(p/'evidence/final-oci.receipt.json').write_text(json.dumps(record,indent=2)+'\n');(out/'manifest.json').write_bytes(manifest.read_bytes());print(json.dumps(record),flush=True)
with (p/'evidence/final-image-pull.log').open('w') as log:subprocess.run(['docker','pull',repo+'@'+digest],check=True,stdout=log,stderr=subprocess.STDOUT)
observed=json.loads(subprocess.check_output(['docker','image','inspect',repo+'@'+digest]))[0]
assert repo+'@'+digest in observed['RepoDigests']
(p/'evidence/final-image-pull.receipt.json').write_text(json.dumps({'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'selectedArtifact':repo+'@'+digest,'imageId':observed['Id'],'exactRepoDigestReadback':True},indent=2)+'\n')
shutil.rmtree(stage)

# Adapted replay operation; not a historical receipt. See provenance.json.
from pathlib import Path, PurePosixPath
import subprocess,json,hashlib,tarfile,io,zipfile,datetime
p=Path('__RUN_ROOT__');h=json.loads((p/'evidence/build-handoff.json').read_text());a=h['capabilityVersion']['artifact'];ref=a['repository']+'@'+a['digest'];name=p.name+'-files-verify'
expected=json.loads((p/'inputs/original-webui-oci.projection.json').read_text())['staticFiles']
with zipfile.ZipFile(p/'inputs/ibd-evidence-assistant-v2-0.2.0-upload-package.zip') as z:carrierExpected={n[len('carrier/'):]: 'sha256:'+hashlib.sha256(z.read(n)).hexdigest() for n in z.namelist() if n.startswith('carrier/') and not n.endswith('/')}
image=json.loads(subprocess.check_output(['docker','image','inspect',ref]))[0];assert ref in image['RepoDigests']
assert subprocess.run(['docker','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
subprocess.run(['docker','create','--name',name,'--network','none','--label','opl.probe='+p.name,'--entrypoint','/bin/true',ref],check=True,stdout=subprocess.DEVNULL)
try:
 c=json.loads(subprocess.check_output(['docker','inspect',name]))[0];assert c['Image']==image['Id']
 for m in c['Mounts']:
  target=Path(m['Destination'])
  assert not any(target==Path(q) or target.is_relative_to(Path(q)) or Path(q).is_relative_to(target) for q in ['/web','/opt/ibd-carrier'])
 results={}
 for root,wanted in [('/web',expected),('/opt/ibd-carrier',carrierExpected)]:
  data=subprocess.check_output(['docker','cp',name+':'+root+'/.','-']);found={}
  with tarfile.open(fileobj=io.BytesIO(data)) as t:
   for member in t:
    rel=str(PurePosixPath(member.name));assert not PurePosixPath(member.name).is_absolute() and '..' not in PurePosixPath(member.name).parts
    if rel in wanted:
     assert member.isfile(), 'expected regular final file: '+rel
     found[rel]='sha256:'+hashlib.sha256(t.extractfile(member).read()).hexdigest()
  assert found==wanted,'merged filesystem mismatch at '+root
  results[root]={'verifiedFiles':len(found),'fileHashes':found,'allExpectedFilesPresentAndRegular':True}
 record={'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'method':'Docker engine merged image filesystem readback via docker create and docker cp; no layer-union approximation','finalImage':ref,'imageId':image['Id'],'sourceOCIManifestDigest':a['digest'],'noMountsOverrideVerifiedPaths':True,'finalFilesystemReadbackMatchesInputs':True,'paths':results,'supersedesFileContentProofIn':'final-oci.receipt.json; manifest/blob/archive digests from that receipt remain verified'}
 (p/'evidence/final-filesystem-readback.receipt.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps({k:v for k,v in record.items() if k!='paths'}),flush=True)
finally:subprocess.run(['docker','rm','-f','-v',name],check=True,stdout=subprocess.DEVNULL)

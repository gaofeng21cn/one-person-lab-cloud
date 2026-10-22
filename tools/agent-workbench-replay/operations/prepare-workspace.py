# Adapted replay operation; not a historical receipt. See provenance.json.
from pathlib import Path
import subprocess,json,socket,time,shutil,os,hashlib,datetime,secrets
p=Path('__RUN_ROOT__')
for name in ['storage','tmp','home','private']:(p/name).mkdir(exist_ok=True)
(p/'private').chmod(0o700)
# Use only the provider configuration already authorized for this dev preview.
config=Path('__MODEL_CONFIG_ON_DEV__')
assert config.is_file();shutil.copyfile(config,p/'private/model-config.toml');(p/'private/model-config.toml').chmod(0o600)
subprocess.run(['truncate','-s','12G',str(p/'quota.img')],check=True)
subprocess.run(['mkfs.ext4','-F','-q','-O','project,quota','-E','quotatype=prjquota',str(p/'quota.img')],check=True)
subprocess.run(['sudo','-n','mount','-o','loop,prjquota',str(p/'quota.img'),str(p/'storage')],check=True)
subprocess.run(['sudo','-n','chown',f'{os.getuid()}:{os.getgid()}',str(p/'storage')],check=True)
pg=p.name+'-pg'
assert subprocess.run(['docker','inspect',pg],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
pg_password=secrets.token_hex(24)
(p/'private/postgres-password').write_text(pg_password);(p/'private/postgres-password').chmod(0o600)
subprocess.run(['docker','run','-d','--name',pg,'--label','opl.probe='+p.name,'-e','POSTGRES_PASSWORD','-p','127.0.0.1::5432','sha256:301bcb60b8a3ee4ab7e147932723e3abd1cef53516ce5210b39fd9fe5e3602ae'],check=True,stdout=subprocess.DEVNULL,env={**os.environ,'POSTGRES_PASSWORD':pg_password})
for _ in range(60):
 if subprocess.run(['docker','exec',pg,'pg_isready','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
 time.sleep(.5)
else:raise RuntimeError('dedicated PostgreSQL not ready')
port=subprocess.check_output(['docker','port',pg,'5432'],text=True).strip().rsplit(':',1)[1]
rec={'timestamp':datetime.datetime.now(datetime.timezone.utc).isoformat(),'postgresContainer':pg,'postgresPort':port,'quotaFilesystemMounted':subprocess.run(['mountpoint','-q',str(p/'storage')]).returncode==0,'modelConfiguration':'private runtime input, excluded from artifacts'}
(p/'evidence/workspace-preflight.json').write_text(json.dumps(rec,indent=2)+'\n');print(json.dumps(rec),flush=True)

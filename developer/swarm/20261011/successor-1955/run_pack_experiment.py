#!/usr/bin/env python3
"""One new tiny, owned RCC pipeline experiment; historical wave is not replayed."""
import hashlib, json, os, stat, subprocess
from pathlib import Path

evidence = Path(__file__).parent
outer = Path('/workspace/rcc-successor-experiments-2015')
scratch = outer / 'pack'
home = outer / 'coordinator-home'
global_ht = Path('/workspace/rcc-successor-lifecycle/global-ht')
binary = Path('/workspace/rcc-successor-tools/pack-e2e')
python = '/opt/codex/runtimes/codex-primary-runtime/dependencies/python/bin/python3'

def guard(path, mode):
    s = path.lstat()
    assert stat.S_ISDIR(s.st_mode) and stat.S_IMODE(s.st_mode) == mode, (str(path), oct(s.st_mode))
    assert (s.st_uid, s.st_gid) == (os.geteuid(), os.getegid()), str(path)
    return {'path': str(path), 'mode': oct(stat.S_IMODE(s.st_mode)), 'uid': s.st_uid, 'gid': s.st_gid}

source = evidence / 'pack-e2e/source/main.go'
assert hashlib.sha256(source.read_bytes()).hexdigest() == 'c1dd426967511e436bc408ad168704685e04359d6be2064e5ddb250d8c1f4b42'
files = [source, binary, evidence/'prototype-overlay.json', evidence/'functions_private_linux.go.txt', evidence/'platform_linux_isolated.go.txt', evidence/'run_bounded_v3.py']
binding = {'build_result': 'pack-e2e/build-4/result.json', 'base_source': '7945f956b428359928908a9f42e36417b113dafd', 'classification': 'private overlaid diagnostic, never shipping binary', 'files': [{'path': str(p), 'bytes': p.stat().st_size, 'sha256': hashlib.sha256(p.read_bytes()).hexdigest()} for p in files]}
(evidence/'pack-e2e/build-binding-4.json').write_text(json.dumps(binding, indent=2)+'\n')
outer.mkdir(mode=0o700)
scratch.mkdir(mode=0o700)
home.mkdir(mode=0o700)
pre = [guard(p, 0o700) for p in (outer, scratch, home)] + [guard(global_ht, 0o750)]
(evidence/'pack-e2e/preflight.json').write_text(json.dumps(pre, indent=2)+'\n')
env = dict(os.environ, ROBOCORP_HOME=str(home), RCC_HOLOTREE_MODE='private', GOMAXPROCS='2', GOMEMLIMIT='1GiB', GOTELEMETRY='off')
cmd = ['python3',str(evidence/'run_bounded_v3.py'),'--out',str(evidence/'pack-e2e/run-1'),'--seconds','90','--scratch-root',str(outer),str(binary),'-mode=run','-root',str(scratch),'-python',python]
r = subprocess.run(cmd, env=env, capture_output=True, text=True)
(evidence/'pack-e2e/launcher.stdout').write_text(r.stdout)
(evidence/'pack-e2e/launcher.stderr').write_text(r.stderr)
post = [guard(p, 0o700) for p in (outer, scratch, home)] + [guard(global_ht, 0o750)]
for p in sorted(scratch.rglob('*')):
    if p.is_dir() and (p.name in ('home','producer-home','workers') or p.name.startswith('pair-')):
        post.append(guard(p, 0o700))
(evidence/'pack-e2e/postflight.json').write_text(json.dumps({'returncode': r.returncode, 'guards': post}, indent=2)+'\n')
assert hashlib.sha256(source.read_bytes()).hexdigest() == binding['files'][0]['sha256']
print(r.stdout, end=''); print(r.stderr, end='')
raise SystemExit(r.returncode)

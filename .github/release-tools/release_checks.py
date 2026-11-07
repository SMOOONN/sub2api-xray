#!/usr/bin/env python3
"""Require successful CI for the exact release commit and push ref."""
import argparse
import json
import os
from pathlib import Path
import re
import time
from urllib.parse import urlencode, quote
from urllib.request import Request, urlopen

import yaml

WORKFLOWS = ('backend-ci.yml', 'security-scan.yml')
# The native macOS job is an explicit workflow_dispatch option, not a push check.
OPTIONAL_PUSH_JOBS = {'backend-ci.yml': {'shell'}}


def required_jobs(root):
    result = {}
    for filename in WORKFLOWS:
        workflow = yaml.safe_load((root / '.github/workflows' / filename).read_text())
        jobs = {}
        for identifier, job in workflow['jobs'].items():
            if identifier in OPTIONAL_PUSH_JOBS.get(filename, set()):
                if 'inputs.run_native_macos == true' not in str(job.get('if', '')):
                    raise ValueError('native macOS optional-job contract changed')
                continue
            name = job.get('name', identifier)
            if job.get('strategy') or '${{' in name:
                raise ValueError(f'{filename}/{identifier} requires an explicit gate update')
            if name in jobs:
                raise ValueError(f'duplicate required job name: {name}')
            jobs[name] = identifier
        if not jobs:
            raise ValueError(f'no required push jobs in {filename}')
        result[filename] = set(jobs)
    return result


def api_json(path):
    headers = {'Accept': 'application/vnd.github+json',
               'User-Agent': 'sub2api-xray-release-gate',
               'X-GitHub-Api-Version': '2022-11-28'}
    token = os.environ.get('GITHUB_TOKEN')
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = Request('https://api.github.com' + path, headers=headers)
    with urlopen(request, timeout=30) as response:
        return json.load(response)


def collection(get, path, key):
    items = []
    separator = '&' if '?' in path else '?'
    for page in range(1, 11):
        data = get(f'{path}{separator}per_page=100&page={page}')
        if not isinstance(data, dict) or not isinstance(data.get(key), list):
            raise ValueError(f'invalid GitHub {key} response')
        batch = data[key]
        items.extend(batch)
        if len(batch) < 100:
            return items
    raise ValueError(f'GitHub {key} pagination limit reached')


def inspect_checks(repository, sha, ref, expected, get=api_json):
    pending = []
    passed = {}
    for workflow, names in expected.items():
        query = urlencode({'head_sha': sha, 'branch': ref, 'event': 'push'})
        runs = collection(get, f'/repos/{repository}/actions/workflows/{quote(workflow, safe="")}/runs?{query}', 'workflow_runs')
        matching = [run for run in runs if run.get('head_sha') == sha
                    and run.get('head_branch') == ref and run.get('event') == 'push']
        if not matching:
            pending.append(f'{workflow}: no matching push run yet')
            continue
        run = max(matching, key=lambda item: int(item['id']))
        if run.get('status') != 'completed':
            pending.append(f'{workflow}: {run.get("status", "pending")}')
            continue
        if run.get('conclusion') != 'success':
            raise ValueError(f'{workflow} run {run["id"]} concluded {run.get("conclusion")}')
        attempt = int(run.get('run_attempt', 1))
        jobs = collection(get, f'/repos/{repository}/actions/runs/{run["id"]}/attempts/{attempt}/jobs', 'jobs')
        for name in sorted(names):
            matches = [job for job in jobs if job.get('name') == name]
            if len(matches) != 1:
                raise ValueError(f'{workflow}: expected exactly one required job {name}')
            job = matches[0]
            if job.get('status') != 'completed' or job.get('conclusion') != 'success':
                raise ValueError(f'{workflow}/{name} did not complete successfully: {job.get("conclusion")}')
        passed[workflow] = {'run_id': run['id'], 'attempt': attempt, 'jobs': sorted(names)}
    return pending, passed


def wait_for_checks(repository, sha, ref, expected, timeout=3600, interval=30,
                    get=api_json, clock=time.monotonic, sleep=time.sleep):
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repository):
        raise ValueError('invalid repository')
    if not re.fullmatch(r'[0-9a-f]{40}', sha) or not ref or ref.startswith('refs/'):
        raise ValueError('an exact commit SHA and short push ref are required')
    if timeout < 0 or interval <= 0:
        raise ValueError('invalid polling bounds')
    deadline = clock() + timeout
    last_pending = None
    while True:
        pending, passed = inspect_checks(repository, sha, ref, expected, get)
        if not pending:
            return {'sha': sha, 'ref': ref, 'workflows': passed}
        if pending != last_pending:
            print('; '.join(pending), flush=True)
            last_pending = pending
        remaining = deadline - clock()
        if remaining <= 0:
            raise TimeoutError('required CI checks did not complete before the release gate deadline')
        sleep(min(interval, remaining))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--sha', required=True)
    parser.add_argument('--ref', required=True)
    parser.add_argument('--root', type=Path, default=Path('.'))
    parser.add_argument('--timeout', type=int, default=3600)
    args = parser.parse_args()
    result = wait_for_checks(os.environ['GITHUB_REPOSITORY'], args.sha, args.ref,
                             required_jobs(args.root), timeout=args.timeout)
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    main()

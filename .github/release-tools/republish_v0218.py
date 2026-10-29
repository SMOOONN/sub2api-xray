"""Temporary controller for the authorized v0.2.18-xray6 replacement."""
import json
import os
from pathlib import Path
import sys
from urllib.error import HTTPError
from urllib.request import Request, urlopen

REPO = 'SMOOONN/sub2api-xray'
TAG = 'v0.2.18-xray6'
OLD_RELEASE = 427402968
OLD_TAG = 'cb13e45563cd388b8dacd32899cf777aa853e1a2'
OLD_ASSETS = {687112182, 687112183, 687112180, 687112181, 687112211, 687112266}
OLD_DIGESTS = {
    'sha256:df103ee43c7d9a40586273d759345f5b4ed682bef4624dc46d4bf3850a096448',
    'sha256:13fe26c2d3c6a1dc0de8a6c51eb5ede54a64939839fbd5f585f555365d53bc91',
    'sha256:e56c465cb14331d7262e7deafc3eda9f69bf5310254f064bfdaf468f2bdb97f8',
    'sha256:ec1a5047a79a96958cde1be792aec36bf5977cb3efbc22adc7c20058c2c311fd',
    'sha256:9cc00a81b95a3f5d0ab50003c007781654a90b4dd5dd0d7a349ce60d7350479f',
    'sha256:b36386bee4bef07827fc63714693e67526be0c829b5a772af4c1451332339bf6',
    'sha256:1499428c555ea1c8a39003c2e19e659a3042ddabdd0e288182aa27b638d8d132',
}
OLD_ALIASES = {'0', '0.2', 'latest', '0.2.18-xray6', '0.2.18-xray6-amd64', '0.2.18-xray6-arm64'}
PACKAGE = '/users/SMOOONN/packages/container/sub2api-xray'


def api(path, method='GET', absent=False):
    request = Request('https://api.github.com' + path, method=method, headers={
        'Authorization': 'Bearer ' + os.environ['GITHUB_TOKEN'],
        'Accept': 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28',
        'User-Agent': 'sub2api-xray-version-replacement',
    })
    try:
        with urlopen(request, timeout=30) as response:
            return json.load(response) if response.status != 204 else None
    except HTTPError as error:
        if absent and error.code == 404:
            return None
        raise RuntimeError(f'GitHub {method} {path}: HTTP {error.code}') from None


def inventory(require_all=True):
    versions = []
    for page in range(1, 21):
        batch = api(f'{PACKAGE}/versions?per_page=100&page={page}')
        versions.extend(batch)
        if len(batch) < 100:
            break
    else:
        raise RuntimeError('Package inventory exceeded the bounded page limit')
    selected = [item for item in versions if item['name'] in OLD_DIGESTS]
    if ((require_all and {item['name'] for item in selected} != OLD_DIGESTS)
            or len({item['name'] for item in selected}) != len(selected)):
        raise RuntimeError('Old image manifest inventory changed')
    if any(set(item['metadata']['container']['tags']) - OLD_ALIASES for item in selected):
        raise RuntimeError('An old manifest is now used by another version')
    for item in versions:
        if item['name'] not in OLD_DIGESTS and set(item['metadata']['container']['tags']) & OLD_ALIASES:
            raise RuntimeError('An expected image alias now points to another manifest')
    return selected


def main(mode):
    if mode not in {'audit', 'withdraw'} or os.environ['GITHUB_REPOSITORY'] != REPO:
        raise RuntimeError('Unexpected operation or repository')
    base = f'/repos/{REPO}'
    release = api(f'{base}/releases/tags/{TAG}', absent=mode == 'withdraw')
    if release is not None and (release['id'] != OLD_RELEASE or release['tag_name'] != TAG
            or release.get('immutable') or release['draft'] or release['prerelease']
            or {asset['id'] for asset in release['assets']} != OLD_ASSETS):
        raise RuntimeError('Old release identity or attachments changed')
    canonical = Path(f'docs/releases/{TAG}_CN.md').read_text(encoding='utf-8')
    if release is not None and release['body'].strip('\r\n') != canonical.strip('\r\n'):
        raise RuntimeError('Old release notes changed')
    tag = api(f'{base}/git/ref/tags/{TAG}', absent=True)
    selected = inventory(require_all=mode == 'audit')
    receipt = {'mode': mode, 'source': os.environ['GITHUB_SHA'], 'release': OLD_RELEASE,
               'versions': [{'id': item['id'], 'digest': item['name'],
                             'tags': item['metadata']['container']['tags']} for item in selected],
               'already_absent': sorted(OLD_DIGESTS - {item['name'] for item in selected}),
               'deleted': []}
    output = Path('republish-receipt.json')

    def save():
        output.write_text(json.dumps(receipt, indent=2) + '\n', encoding='utf-8')

    save()
    if mode == 'audit':
        if not tag or tag['object']['sha'] != OLD_TAG:
            raise RuntimeError('Old signed tag changed')
        print(f'Verified old release, six assets and {len(selected)} image manifests.')
        return
    event = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
    if (os.environ['GITHUB_EVENT_NAME'] != 'delete' or os.environ['GITHUB_ACTOR'] != 'SMOOONN'
            or event.get('ref_type') != 'tag' or event.get('ref') != TAG or tag is not None):
        raise RuntimeError('Withdrawal requires the authorized exact tag deletion event')
    sha = os.environ['GITHUB_SHA']
    if api(f'{base}/git/ref/heads/main')['object']['sha'] != sha:
        raise RuntimeError('Default branch moved during the replacement')
    for workflow in ('backend-ci.yml', 'security-scan.yml', 'republish-v0218.yml'):
        runs = api(f'{base}/actions/workflows/{workflow}/runs?head_sha={sha}&event=push&per_page=20')['workflow_runs']
        if not runs or runs[0]['status'] != 'completed' or runs[0]['conclusion'] != 'success':
            raise RuntimeError(f'Required verification has not passed: {workflow}')
    # All identity and verification checks finish before any remote deletion.
    for item in selected:
        current = api(f'{PACKAGE}/versions/{item["id"]}', absent=True)
        if current is not None:
            if current['name'] != item['name'] or set(current['metadata']['container']['tags']) - OLD_ALIASES:
                raise RuntimeError('Image version changed immediately before deletion')
            api(f'{PACKAGE}/versions/{item["id"]}', 'DELETE', absent=True)
        receipt['deleted'].append({'version': item['id'], 'digest': item['name']})
        save()
    if release is not None:
        api(f'{base}/releases/{OLD_RELEASE}', 'DELETE', absent=True)
        receipt['deleted'].append({'release': OLD_RELEASE})
        save()
    if api(f'{base}/releases/{OLD_RELEASE}', absent=True) is not None:
        raise RuntimeError('Old release still exists')
    for item in selected:
        if api(f'{PACKAGE}/versions/{item["id"]}', absent=True) is not None:
            raise RuntimeError('An old image version still exists')
    receipt['verified_absent'] = True
    save()
    print('Verified withdrawal of the old release and seven exact image manifests.')


if __name__ == '__main__':
    main(sys.argv[1])

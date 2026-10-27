import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import yaml

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('release_matrix', Path(__file__).with_name('release_matrix.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseMatrixTest(unittest.TestCase):
    def setUp(self):
        self.previous = Path.cwd()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        os.chdir(self.temp.name)
        self.addCleanup(os.chdir, self.previous)
        for name in ('.goreleaser.yaml', '.goreleaser.simple.yaml'):
            shutil.copyfile(ROOT / name, name)
        Path('backend/cmd/server').mkdir(parents=True)
        release.VERSION_FILE.write_text('9.8.7\n')

    def fixture_artifacts(self, simple=False):
        directory = Path('release-input')
        directory.mkdir()
        for target in release.targets(simple):
            name = release.archive_name('9.8.7', target)
            archive = directory / name
            if target['goos'] == 'linux':
                with tarfile.open(archive, 'w:gz') as out:
                    info = tarfile.TarInfo('sub2api')
                    info.size = 7
                    info.mode = 0o755
                    out.addfile(info, io.BytesIO(b'fixture'))
            else:
                archive.write_bytes(b'fixture archive')
            metadata = {'version': '9.8.7', 'sha': 'a' * 40, 'target': target,
                        'archive': name, 'sha256': release.sha256(archive)}
            (directory / f"manifest-{target['goos']}-{target['goarch']}.json").write_text(json.dumps(metadata))
        return argparse.Namespace(input='release-input', version='9.8.7', sha='a' * 40, simple=simple, output='contexts')

    def test_full_and_simple_matrix_match_existing_targets(self):
        full = release.targets()
        self.assertEqual(len(full), 5)
        self.assertNotIn({'goos': 'windows', 'goarch': 'arm64'}, full)
        self.assertEqual(release.targets(True), [{'goos': 'linux', 'goarch': 'amd64'}])

    def test_tag_release_does_not_inherit_a_simple_release_repository_default(self):
        workflow = yaml.load((ROOT / '.github/workflows/release.yml').read_text(), Loader=yaml.BaseLoader)
        simple_setting = workflow['env']['SIMPLE_RELEASE']
        self.assertNotIn('vars.SIMPLE_RELEASE', simple_setting)
        self.assertIn("github.event_name == 'workflow_dispatch'", simple_setting)
        self.assertEqual(workflow['on']['workflow_dispatch']['inputs']['simple_release']['default'], 'false')
        self.assertEqual(len(release.targets()), 5)

    def test_notes_accept_exact_unsigned_and_ssh_signed_messages(self):
        canonical = b'# Release 9.8.7\n\nRelease notes.\n'
        notes_file = Path('docs/releases/v9.8.7_CN.md')
        notes_file.parent.mkdir(parents=True)
        notes_file.write_bytes(canonical)
        header = b'object ' + b'a' * 40 + b'\ntype commit\ntag v9.8.7\ntagger Example <example@example.com> 1 +0000\n\n'
        signature = b'-----BEGIN SSH SIGNATURE-----\nfixture\n-----END SSH SIGNATURE-----\n'
        for suffix in (b'', signature):
            with self.subTest(signed=bool(suffix)):
                with patch.object(release.subprocess, 'check_output', return_value=header + canonical + suffix):
                    release.notes(argparse.Namespace(tag='v9.8.7'))
        self.assertEqual(notes_file.read_bytes(), canonical)

    def test_notes_reject_body_and_line_ending_changes_before_the_signature(self):
        canonical = b'# Release 9.8.7\n\nRelease notes.\n'
        notes_file = Path('docs/releases/v9.8.7_CN.md')
        notes_file.parent.mkdir(parents=True)
        notes_file.write_bytes(canonical)
        header = b'object ' + b'a' * 40 + b'\ntype commit\ntag v9.8.7\n\n'
        signature = b'-----BEGIN SSH SIGNATURE-----\nfixture\n-----END SSH SIGNATURE-----\n'
        for message in (canonical.replace(b'Release notes.', b'Changed notes.'), canonical + b'\n', canonical.replace(b'\n', b'\r\n')):
            with self.subTest(message=message):
                with patch.object(release.subprocess, 'check_output', return_value=header + message + signature):
                    with self.assertRaisesRegex(ValueError, 'differs from the canonical'):
                        release.notes(argparse.Namespace(tag='v9.8.7'))

    def test_notes_reject_an_ssh_signature_inside_the_canonical_document(self):
        canonical = b'# Release 9.8.7\n\n-----BEGIN SSH SIGNATURE-----\nfixture\n-----END SSH SIGNATURE-----\n'
        notes_file = Path('docs/releases/v9.8.7_CN.md')
        notes_file.parent.mkdir(parents=True)
        notes_file.write_bytes(canonical)
        with patch.object(release.subprocess, 'check_output', return_value=b'tag v9.8.7\n\n' + canonical):
            with self.assertRaisesRegex(ValueError, 'must not contain an SSH signature'):
                release.notes(argparse.Namespace(tag='v9.8.7'))

    def test_preflight_rejects_the_previous_updater_account_and_missing_runtime(self):
        filenames = [
            'backend/internal/service/update_service.go',
            'frontend/src/components/common/VersionBadge.vue',
            'deploy/install.sh',
            'Dockerfile.goreleaser',
        ]
        for filename in filenames:
            destination = Path(filename)
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / filename, destination)
        with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'SMOOONN/sub2api-xray'}):
            release.preflight(argparse.Namespace())
            updater = Path(filenames[0])
            original = updater.read_text()
            updater.write_text(original.replace('SMOOONN/sub2api-xray', 'smmooooonn/sub2api-xray'))
            with self.assertRaisesRegex(ValueError, 'publishing repository'):
                release.preflight(argparse.Namespace())
            updater.write_text(original)
            runtime = Path('Dockerfile.goreleaser')
            runtime.write_text(runtime.read_text().replace(
                'COPY --from=sing-box-runtime /opt/sing-box/sing-box /usr/local/bin/sing-box',
                '# sing-box copy removed'))
            with self.assertRaisesRegex(ValueError, 'missing the sing-box-runtime binary'):
                release.preflight(argparse.Namespace())

    def test_leaf_keeps_packaging_and_selects_only_one_target(self):
        original = release.config()
        release.generate_config(argparse.Namespace(mode='build', simple=False, goos='darwin', goarch='arm64', output='leaf.yaml'))
        leaf = yaml.safe_load(Path('leaf.yaml').read_text())
        self.assertEqual(leaf['builds'][0]['goos'], ['darwin'])
        self.assertEqual(leaf['builds'][0]['goarch'], ['arm64'])
        self.assertEqual(leaf['builds'][0]['ignore'], [])
        self.assertEqual(leaf['archives'], original['archives'])
        self.assertEqual(leaf['release'], original['release'])
        self.assertFalse(leaf['dockers'])
        self.assertIn('{{ .Env.RELEASE_DATE }}', '\n'.join(leaf['builds'][0]['ldflags']))

    def test_publication_config_has_no_compilation_or_docker_work(self):
        for simple in (False, True):
            with self.subTest(simple=simple):
                original = release.config(simple)
                release.generate_config(argparse.Namespace(mode='publish', simple=simple, output='publisher.yaml'))
                data = yaml.safe_load(Path('publisher.yaml').read_text())
                self.assertTrue(data['builds'][0]['skip'])
                self.assertFalse(data['archives'])
                self.assertFalse(data['dockers'])
                self.assertEqual(data['release']['header'], original['release']['header'])
                self.assertEqual(data['release']['footer'], original['release']['footer'])
                if simple:
                    self.assertTrue(data['checksum']['disable'])
                    self.assertTrue(data['release']['skip_upload'])
                else:
                    self.assertEqual(data['checksum']['extra_files'], data['release']['extra_files'])

    def test_collect_and_verify_hash_and_source_binding(self):
        args = self.fixture_artifacts()
        release.verify(args)
        file = next(Path(args.input).glob('*.tar.gz'))
        file.write_bytes(b'corrupted')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            release.verify(args)

    def test_missing_extra_and_wrong_commit_artifacts_are_rejected(self):
        args = self.fixture_artifacts(True)
        args.sha = 'b' * 40
        with self.assertRaises(ValueError):
            release.verify(args)
        args.sha = 'a' * 40
        Path('release-input/unexpected').write_text('not an asset')
        with self.assertRaises(ValueError):
            release.verify(args)
        Path('release-input/unexpected').unlink()
        next(Path('release-input').glob('*.tar.gz')).unlink()
        with self.assertRaises(FileNotFoundError):
            release.verify(args)

    def test_linux_context_preserves_binary_executable_mode(self):
        args = self.fixture_artifacts()
        Path('Dockerfile.goreleaser').write_text('FROM scratch\nCOPY sub2api /sub2api\n')
        Path('deploy').mkdir()
        Path('deploy/docker-entrypoint.sh').write_text('#!/bin/sh\nexec /app/sub2api\n')
        Path('backend/resources').mkdir()
        Path('backend/resources/data').write_text('fixture')
        release.contexts(args)
        for arch in ('amd64', 'arm64'):
            binary = Path('contexts') / arch / 'sub2api'
            self.assertEqual(binary.read_bytes(), b'fixture')
            self.assertEqual(binary.stat().st_mode & 0o777, 0o755)

    def test_plan_requires_a_tag_for_publication(self):
        args = argparse.Namespace(ref='main', dry_run=False, simple=False)
        with patch.object(subprocess, 'check_output', return_value='a' * 40 + '\n'):
            with self.assertRaisesRegex(ValueError, 'version tag'):
                release.plan(args)
        args.ref = 'v9.8.7'
        with patch.object(subprocess, 'check_output', side_effect=['a' * 40 + '\n', 'b' * 40 + '\n']):
            with self.assertRaisesRegex(ValueError, 'does not match'):
                release.plan(args)

    def test_publication_preserves_source_and_requires_matching_annotated_tag(self):
        args = argparse.Namespace(ref='v9.8.7', dry_run=False, simple=False)
        for source, tag_type, error in [('9.8.6\n', 'tag', 'VERSION file'), ('9.8.7\n', 'commit', 'annotated')]:
            with self.subTest(source=source, tag_type=tag_type):
                release.VERSION_FILE.write_text(source)
                with patch.object(subprocess, 'check_output', side_effect=['a' * 40, 'a' * 40, tag_type]):
                    with self.assertRaisesRegex(ValueError, error):
                        release.plan(args)
                self.assertEqual(release.VERSION_FILE.read_text(), source)
        release.VERSION_FILE.write_bytes(b'9.8.7\r\n')
        with patch.dict(os.environ, {'GITHUB_OUTPUT': 'outputs'}), patch.object(subprocess, 'check_output', side_effect=['a' * 40, 'a' * 40, 'tag']):
            release.plan(args)
        self.assertEqual(release.VERSION_FILE.read_bytes(), b'9.8.7\r\n')
    def test_dry_run_plan_resolves_matrix_without_a_new_tag(self):
        with patch.dict(os.environ, {'GITHUB_OUTPUT': 'outputs', 'GITHUB_REPOSITORY_OWNER': 'ExampleOwner'}), patch.object(subprocess, 'check_output', return_value='a' * 40 + '\n'):
            release.plan(argparse.Namespace(ref='feature/matrix', dry_run=True, simple=False))
        output = dict(line.split('=', 1) for line in Path('outputs').read_text().splitlines())
        self.assertEqual(output['dry_run'], 'true')
        self.assertEqual(output['owner_lower'], 'exampleowner')
        self.assertEqual(len(json.loads(output['matrix'])['include']), 5)

    def test_docker_commands_do_not_publish_during_dry_run(self):
        fake_bin = Path('bin')
        fake_bin.mkdir()
        docker = fake_bin / 'docker'
        docker.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_LOG"\n')
        docker.chmod(0o755)
        env = {**os.environ, 'PATH': str(fake_bin.resolve()) + os.pathsep + os.environ['PATH'],
               'DOCKER_LOG': str(Path('docker.log').resolve()), 'RUNNER_TEMP': self.temp.name,
               'RELEASE_VERSION': '9.8.7', 'RELEASE_SHA': 'a' * 40, 'GITHUB_REPOSITORY': 'ExampleOwner/sub2api-xray',
               'DRY_RUN': 'true', 'SIMPLE_RELEASE': 'false', 'DOCKERHUB_USERNAME': 'skip'}
        subprocess.run(['bash', str(ROOT / '.github/release-tools/release-images.sh')], env=env, check=True)
        log = Path('docker.log').read_text()
        self.assertEqual(log.count('buildx build'), 2)
        self.assertIn('linux/arm64', log)
        self.assertNotIn('--push', log)
        self.assertNotIn('imagetools', log)
        self.assertNotIn('skip/sub2api', log)
        self.assertIn('ghcr.io/exampleowner/sub2api-xray', log)


    def test_published_full_and_simple_image_tags(self):
        fake_bin = Path('bin')
        fake_bin.mkdir()
        docker = fake_bin / 'docker'
        docker.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_LOG"\n')
        docker.chmod(0o755)
        for simple in (False, True):
            with self.subTest(simple=simple):
                log_path = Path(f'docker-{simple}.log').resolve()
                env = {**os.environ, 'PATH': str(fake_bin.resolve()) + os.pathsep + os.environ['PATH'],
                       'DOCKER_LOG': str(log_path), 'RUNNER_TEMP': self.temp.name,
                       'RELEASE_VERSION': '9.8.7', 'RELEASE_SHA': 'a' * 40, 'GITHUB_REPOSITORY': 'ExampleOwner/sub2api-xray',
                       'DRY_RUN': 'false', 'SIMPLE_RELEASE': str(simple).lower(), 'DOCKERHUB_USERNAME': 'fixturehub'}
                subprocess.run(['bash', str(ROOT / '.github/release-tools/release-images.sh')], env=env, check=True)
                log = log_path.read_text()
                self.assertIn('--push', log)
                self.assertEqual(log.count('buildx build'), 1 if simple else 2)
                if simple:
                    self.assertNotIn('fixturehub', log)
                    self.assertNotIn('imagetools', log)
                    self.assertIn('ghcr.io/exampleowner/sub2api-xray:latest', log)
                else:
                    self.assertEqual(log.count('imagetools create'), 2)
                    self.assertIn('fixturehub/sub2api:9.8', log)
                    self.assertIn('ghcr.io/exampleowner/sub2api-xray:9', log)



if __name__ == '__main__':
    unittest.main()

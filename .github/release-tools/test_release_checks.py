import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest
from urllib.parse import parse_qs, urlsplit

import yaml

spec = importlib.util.spec_from_file_location('release_checks', Path(__file__).with_name('release_checks.py'))
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)


class ReleaseChecksTest(unittest.TestCase):
    def setUp(self):
        self.sha = 'a' * 40
        self.ref = 'v9.8.7'
        self.expected = {'backend-ci.yml': {'test', 'frontend'}}
        self.runs = [{'id': 42, 'head_sha': self.sha, 'head_branch': self.ref,
                      'event': 'push', 'status': 'completed', 'conclusion': 'success', 'run_attempt': 1}]
        self.jobs = [{'name': name, 'status': 'completed', 'conclusion': 'success'}
                     for name in ('test', 'frontend')]
        self.paths = []

    def api(self, path):
        self.paths.append(path)
        if '/workflows/' in path:
            return {'workflow_runs': copy.deepcopy(self.runs)}
        return {'jobs': copy.deepcopy(self.jobs)}

    def inspect(self):
        return checks.inspect_checks('Example/repo', self.sha, self.ref, self.expected, self.api)

    def test_accepts_only_matching_successful_run_and_required_jobs(self):
        pending, passed = self.inspect()
        self.assertEqual(pending, [])
        self.assertEqual(passed['backend-ci.yml']['run_id'], 42)
        self.assertEqual(passed['backend-ci.yml']['jobs'], ['frontend', 'test'])
        self.assertTrue(any('/attempts/1/jobs?' in path for path in self.paths))

    def test_other_sha_ref_or_event_cannot_authorize_release(self):
        for field, value in [('head_sha', 'b' * 40), ('head_branch', 'main'), ('event', 'pull_request')]:
            with self.subTest(field=field):
                original = self.runs[0][field]
                self.runs[0][field] = value
                pending, passed = self.inspect()
                self.assertTrue(pending)
                self.assertEqual(passed, {})
                self.runs[0][field] = original

    def test_latest_run_supersedes_older_success(self):
        self.runs.append({**self.runs[0], 'id': 43, 'status': 'queued', 'conclusion': None})
        pending, passed = self.inspect()
        self.assertTrue(pending)
        self.assertEqual(passed, {})
        self.runs[-1].update(status='completed', conclusion='failure')
        with self.assertRaisesRegex(ValueError, 'concluded failure'):
            self.inspect()

    def test_failed_cancelled_skipped_or_missing_conclusion_is_rejected(self):
        for conclusion in ('failure', 'cancelled', 'skipped', 'timed_out', None):
            with self.subTest(conclusion=conclusion):
                self.runs[0]['conclusion'] = conclusion
                with self.assertRaises(ValueError):
                    self.inspect()

    def test_required_jobs_cannot_be_missing_skipped_or_duplicated(self):
        original = copy.deepcopy(self.jobs)
        for jobs in (original[:1], original + [original[0]],
                     [{**original[0], 'conclusion': 'skipped'}, original[1]],
                     [{**original[0], 'status': 'in_progress'}, original[1]]):
            with self.subTest(jobs=jobs):
                self.jobs = jobs
                with self.assertRaises(ValueError):
                    self.inspect()

    def test_new_attempt_does_not_reuse_previous_attempt_jobs(self):
        self.runs[0]['run_attempt'] = 3
        self.inspect()
        self.assertTrue(any('/attempts/3/jobs?' in path for path in self.paths))

    def test_malformed_api_response_is_not_an_empty_success(self):
        with self.assertRaisesRegex(ValueError, 'invalid GitHub'):
            checks.inspect_checks('Example/repo', self.sha, self.ref, self.expected,
                                  lambda _: {'message': 'API unavailable'})

    def test_collection_reads_all_pages(self):
        calls = []

        def api(path):
            calls.append(path)
            page = parse_qs(urlsplit(path).query)['page'][0]
            return {'jobs': [None] * 100 if page == '1' else ['last']}

        self.assertEqual(len(checks.collection(api, '/jobs', 'jobs')), 101)
        self.assertEqual(len(calls), 2)

    def test_missing_run_times_out_instead_of_allowing_build(self):
        self.runs = []
        clock = iter((0, 1)).__next__
        with self.assertRaises(TimeoutError):
            checks.wait_for_checks('Example/repo', self.sha, self.ref, self.expected,
                                   timeout=0, get=self.api, clock=clock, sleep=lambda _: None)

    def test_polling_waits_until_the_matching_run_completes(self):
        sleeps = []
        self.runs[0].update(status='in_progress', conclusion=None)

        def sleep(seconds):
            sleeps.append(seconds)
            self.runs[0].update(status='completed', conclusion='success')

        result = checks.wait_for_checks('Example/repo', self.sha, self.ref, self.expected,
                                        timeout=10, interval=1, get=self.api, clock=lambda: 0, sleep=sleep)
        self.assertEqual(result['sha'], self.sha)
        self.assertEqual(sleeps, [1])

    def test_required_jobs_follow_workflow_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            folder = root / '.github/workflows'
            folder.mkdir(parents=True)
            for filename in checks.WORKFLOWS:
                jobs = {'mandatory': {'runs-on': 'ubuntu-latest'}}
                if filename == 'backend-ci.yml':
                    jobs['shell'] = {'if': "github.event_name == 'workflow_dispatch' && inputs.run_native_macos == true"}
                (folder / filename).write_text(yaml.safe_dump({'jobs': jobs}))
            self.assertEqual(checks.required_jobs(root), {filename: {'mandatory'} for filename in checks.WORKFLOWS})
            path = folder / 'backend-ci.yml'
            data = yaml.safe_load(path.read_text())
            data['jobs']['new-check'] = {'name': 'New required check'}
            path.write_text(yaml.safe_dump(data))
            self.assertIn('New required check', checks.required_jobs(root)['backend-ci.yml'])


if __name__ == '__main__':
    unittest.main()

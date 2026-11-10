import base64
import contextlib
import io
import json
import subprocess
import unittest
from unittest.mock import patch

import register_app as setup


class RegistrationTests(unittest.TestCase):
    def setUp(self):
        self.registration = setup.Registration('http://127.0.0.1:9876')
        self.query = {'state': [self.registration.state]}
        self.pem = '-----BEGIN PRIVATE KEY-----\nTEST-KEY-NOT-A-CREDENTIAL\n-----END PRIVATE KEY-----\n'
        self.app = {'id': 123, 'slug': 'rcc-release-test', 'pem': self.pem,
                    'owner': {'login': 'joshyorko'}, 'permissions': dict(setup.PERMISSIONS)}
        self.permissions = {**setup.PERMISSIONS, 'metadata': 'read'}
        self.selection = 'selected'
        self.repositories = ['joshyorko/rcc']
        self.stored = set()
        self.calls = []
        self.secret_inputs = []
        self.install_query = {**self.query, 'installation_id': ['456']}

    def api(self, method, path, token=None, body=None):
        self.calls.append((method, path, body))
        if path.startswith('/app-manifests/'):
            return dict(self.app)
        if path == '/app':
            return {'id': 123, 'owner': {'login': 'joshyorko'}, 'permissions': self.permissions}
        if path == '/app/installations/456':
            return {'app_id': 123, 'account': {'login': 'joshyorko'},
                    'repository_selection': self.selection, 'permissions': self.permissions}
        if path.endswith('/access_tokens'):
            return {'token': 'test-installation-token'}
        if path.startswith('/installation/repositories'):
            return {'total_count': len(self.repositories),
                    'repositories': [{'full_name': name} for name in self.repositories]}
        if path == '/installation/token' and method == 'DELETE':
            return None
        self.fail('Unexpected endpoint ' + path)

    def gh(self, args, value=None):
        if args[0] == 'api':
            return json.dumps({'total_count': len(self.stored), 'secrets': [{'name': name} for name in self.stored]}).encode()
        self.assertEqual(args[:2], ['secret', 'set'])
        self.assertNotIn(self.pem, repr(args))
        self.assertNotIn('TEST-KEY-NOT-A-CREDENTIAL', repr(args))
        self.secret_inputs.append((args[2], value))
        self.stored.add(args[2])
        return b''

    def register(self):
        return self.registration.register({**self.query, 'code': ['a' * 40]})

    def install(self):
        with patch.object(setup, 'api', side_effect=self.api), patch.object(setup, 'jwt', return_value='test-jwt'), patch.object(setup, 'gh', side_effect=self.gh):
            if self.registration.app is None:
                self.register()
            return self.registration.install(self.install_query)

    def test_manifest_is_private_minimum_no_events_and_bound_callbacks(self):
        manifest = self.registration.manifest()
        self.assertFalse(manifest['public'])
        self.assertEqual(manifest['default_permissions'], setup.PERMISSIONS)
        self.assertEqual(manifest['default_events'], [])
        self.assertFalse(manifest['hook_attributes']['active'])
        self.assertFalse(manifest['request_oauth_on_install'])
        self.assertTrue(manifest['setup_url'].endswith(self.registration.state))

    def test_real_app_response_does_not_require_nonexistent_public_field(self):
        with patch.object(setup, 'api', side_effect=self.api):
            self.assertEqual(self.register(), 'https://github.com/apps/rcc-release-test/installations/new')
            self.assertEqual(set(self.registration.app), {'id', 'slug', 'pem'})
            with self.assertRaisesRegex(RuntimeError, 'duplicate'):
                self.register()

    def test_state_replay_and_missing_codes_deny_before_api(self):
        with patch.object(setup, 'api') as api:
            for query in ({}, {'state': ['wrong'], 'code': ['a' * 40]}, self.query,
                          {**self.query, 'code': ['../../secret']}, {**self.query, 'state': [self.registration.state] * 2}):
                with self.assertRaises(RuntimeError): self.registration.register(query)
            api.assert_not_called()

    def test_wrong_owner_or_extra_permissions_deny(self):
        with patch.object(setup, 'api', side_effect=self.api):
            self.app['owner'] = {'login': 'someone-else'}
            with self.assertRaises(RuntimeError): self.register()
            self.app['owner'] = {'login': 'joshyorko'}
            self.app['permissions']['administration'] = 'write'
            with self.assertRaises(RuntimeError): self.register()
            self.assertIsNone(self.registration.app)

    def test_only_rcc_installation_stores_key_via_stdin_without_output_or_activation(self):
        captured = io.StringIO()
        with contextlib.redirect_stdout(captured):
            result = self.install()
        self.assertFalse(result['automaticMaintenanceEnabled'])
        self.assertTrue(self.registration.complete)
        self.assertEqual(self.registration.app['pem'], '')
        self.assertEqual(set(self.stored), set(setup.SECRET_NAMES))
        self.assertEqual(self.secret_inputs[0], ('RCC_RELEASE_APP_PRIVATE_KEY', self.pem.encode()))
        self.assertNotIn(self.pem, captured.getvalue())
        self.assertEqual(self.calls[-1][:2], ('DELETE', '/installation/token'))

    def test_all_repositories_and_other_installation_scope_deny(self):
        self.selection = 'all'
        with self.assertRaisesRegex(RuntimeError, 'selected RCC'): self.install()
        self.assertFalse(self.secret_inputs)
        self.assertFalse(any(path.endswith('/access_tokens') for _, path, _ in self.calls))

    def test_additional_repositories_deny_and_revoke_token_before_secret_writes(self):
        self.repositories.append('joshyorko/another-repo')
        with self.assertRaisesRegex(RuntimeError, 'ONLY'): self.install()
        self.assertFalse(self.secret_inputs)
        self.assertEqual(self.calls[-1][:2], ('DELETE', '/installation/token'))

    def test_existing_secrets_are_never_overwritten(self):
        self.stored.add('RCC_RELEASE_APP_ID')
        with self.assertRaisesRegex(RuntimeError, 'already exist'): self.install()
        self.assertFalse(self.secret_inputs)

    def test_changed_installed_permissions_deny(self):
        self.permissions['actions'] = 'write'
        with self.assertRaisesRegex(RuntimeError, 'minimum'): self.install()
        self.assertFalse(self.secret_inputs)

    def test_failed_api_never_logs_code_or_response(self):
        with patch.object(setup.urllib.request, 'build_opener', side_effect=RuntimeError(self.pem)):
            with self.assertRaises(RuntimeError) as error:
                setup.api('POST', '/app-manifests/' + 'a' * 40 + '/conversions')
            self.assertNotIn('a' * 40, str(error.exception))
            self.assertNotIn(self.pem, str(error.exception))

    def test_real_rsa_signing_passes_key_by_pipe_and_bounds_jwt_lifetime(self):
        pem = subprocess.run(['openssl', 'genpkey', '-algorithm', 'RSA', '-pkeyopt', 'rsa_keygen_bits:2048'],
                             stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True, timeout=30).stdout.decode()
        token = setup.jwt(123, pem)
        parts = token.split('.')
        decode = lambda part: base64.urlsafe_b64decode(part + '=' * (-len(part) % 4))
        self.assertEqual(json.loads(decode(parts[0]))['alg'], 'RS256')
        claims = json.loads(decode(parts[1]))
        self.assertEqual(claims['iss'], '123')
        self.assertEqual(claims['exp'] - claims['iat'], 600)
        self.assertEqual(len(decode(parts[2])), 256)
        self.assertNotIn('PRIVATE', token)


if __name__ == '__main__':
    unittest.main()

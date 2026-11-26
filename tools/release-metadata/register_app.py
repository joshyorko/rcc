"""One-time RCC App manifest registration. Credentials stay in memory or gh stdin.

Run locally, authorize the manifest in your own GitHub account, then install on
ONLY joshyorko/rcc. This does not activate maintenance or dispatch any workflow.
"""
import base64
import hmac
import html
import json
import os
import re
import secrets
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer

REPOSITORY = 'joshyorko/rcc'
PERMISSIONS = {'contents': 'write', 'pull_requests': 'write', 'actions': 'read'}
SECRET_NAMES = ('RCC_RELEASE_APP_PRIVATE_KEY', 'RCC_RELEASE_APP_ID')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def api(method, path, token=None, body=None):
    require(path.startswith('/') and not path.startswith('//'), 'Invalid GitHub API path')
    headers = {'Accept': 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28'}
    if token:
        headers['Authorization'] = f'Bearer {token}'
    data = None if body is None else json.dumps(body).encode()
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *_):
            return None
    request = urllib.request.Request('https://api.github.com' + path, method=method, headers=headers, data=data)
    try:
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=30) as response:
            return json.loads(response.read()) if response.status != 204 else None
    except Exception:
        # Conversion URLs contain one-use codes; never print URL/request/body/errors.
        raise RuntimeError('GitHub API request failed; no credential details logged') from None


def gh(arguments, value=None):
    result = subprocess.run(['gh', *arguments], input=value, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    require(result.returncode == 0, 'GitHub CLI operation failed; no credential details logged')
    return result.stdout


def stored_secrets():
    data = json.loads(gh(['api', f'repos/{REPOSITORY}/actions/secrets?per_page=100']))
    require(data.get('total_count') == len(data['secrets']), 'Secret inventory is incomplete; refusing replacement')
    return {entry['name'] for entry in data['secrets']}


def jwt(app_id, pem):
    encode = lambda value: base64.urlsafe_b64encode(value).rstrip(b'=')
    now = int(time.time())
    payload = encode(b'{"alg":"RS256","typ":"JWT"}') + b'.' + encode(json.dumps({
        'iat': now - 60, 'exp': now + 540, 'iss': str(app_id),
    }, separators=(',', ':')).encode())
    # OpenSSL reads the key through an inherited pipe, never a file or argv.
    read_fd, write_fd = os.pipe()
    try:
        os.write(write_fd, pem.encode())
        os.close(write_fd)
        write_fd = None
        result = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', f'/dev/fd/{read_fd}'],
            input=payload, pass_fds=(read_fd,), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
        require(result.returncode == 0, 'App key signing failed; no key material logged')
        return (payload + b'.' + encode(result.stdout)).decode()
    finally:
        os.close(read_fd)
        if write_fd is not None:
            os.close(write_fd)


def validate_permissions(actual):
    require(isinstance(actual, dict), 'App permissions are unreadable')
    require({key: value for key, value in actual.items() if key != 'metadata'} == PERMISSIONS
            and actual.get('metadata', 'read') == 'read', 'App permissions differ from the reviewed minimum')


class Registration:
    def __init__(self, base_url):
        self.base_url = base_url
        self.state = secrets.token_urlsafe(32)
        self.app = None
        self.complete = False
        self.written = set()

    def manifest(self):
        return {'name': 'RCC Release Metadata - joshyorko', 'url': 'https://github.com/' + REPOSITORY,
            'description': 'Dedicated RCC release metadata PRs. Protected publication stays separate.',
            'public': False, 'default_permissions': dict(PERMISSIONS), 'default_events': [],
            'hook_attributes': {'url': 'https://github.com/' + REPOSITORY, 'active': False},
            'redirect_url': self.base_url + '/registered',
            'setup_url': self.base_url + '/installed?state=' + self.state,
            'request_oauth_on_install': False, 'setup_on_update': False}

    def check_state(self, query):
        require(len(query.get('state', [])) == 1 and hmac.compare_digest(query['state'][0], self.state),
                'Registration state mismatch; no credentials changed')

    def register(self, query):
        self.check_state(query)
        require(self.app is None, 'App already registered; do not create a duplicate')
        code = query.get('code', [])
        require(len(code) == 1 and re.fullmatch(r'[A-Za-z0-9_-]{20,200}', code[0]), 'Invalid one-use registration code')
        response = api('POST', f'/app-manifests/{code[0]}/conversions')
        require(type(response.get('id')) is int and response['id'] > 0, 'App ID is missing')
        require(response.get('owner', {}).get('login') == 'joshyorko', 'Register this private App under joshyorko')
        validate_permissions(response.get('permissions'))
        require(re.fullmatch(r'[a-z0-9-]+', response.get('slug', '')), 'App slug is invalid')
        pem = response.get('pem', '')
        require(isinstance(pem, str) and len(pem) < 8192 and 'PRIVATE KEY-----' in pem, 'App PEM is missing or invalid')
        # Discard unused client/webhook secrets; no raw response is saved or logged.
        self.app = {key: response[key] for key in ('id', 'slug', 'pem')}
        return f"https://github.com/apps/{self.app['slug']}/installations/new"

    def install(self, query):
        self.check_state(query)
        require(self.app is not None and not self.complete, 'No pending App installation')
        installation_id = query.get('installation_id', [])
        require(len(installation_id) == 1 and re.fullmatch(r'[1-9][0-9]*', installation_id[0]), 'Invalid installation ID')
        app_token = jwt(self.app['id'], self.app['pem'])
        identity = api('GET', '/app', app_token)
        require(identity.get('id') == self.app['id'] and identity.get('owner', {}).get('login') == 'joshyorko', 'Key/App identity mismatch')
        validate_permissions(identity.get('permissions'))
        installation = api('GET', '/app/installations/' + installation_id[0], app_token)
        require(installation.get('app_id') == self.app['id']
                and installation.get('account', {}).get('login') == 'joshyorko'
                and installation.get('repository_selection') == 'selected', 'Install on selected RCC repository only')
        validate_permissions(installation.get('permissions'))
        response = api('POST', '/app/installations/' + installation_id[0] + '/access_tokens',
                       app_token, {'permissions': PERMISSIONS})
        token = response.get('token')
        require(isinstance(token, str) and token, 'Installation token is missing')
        try:
            repositories = api('GET', '/installation/repositories?per_page=100', token)
            require(repositories.get('total_count') == 1 and
                    [repo.get('full_name') for repo in repositories.get('repositories', [])] == [REPOSITORY],
                    'Installation must grant ONLY joshyorko/rcc; repository secrets were not changed')
        finally:
            api('DELETE', '/installation/token', token)
        # Never replace another setup's existing credentials. A partial successful
        # write within this same registration can be retried with the same key.
        require(not ((stored_secrets() & set(SECRET_NAMES)) - self.written),
                'Release credentials already exist; inspect them instead of overwriting')
        for name, value in ((SECRET_NAMES[0], self.app['pem']), (SECRET_NAMES[1], str(self.app['id']))):
            if name not in self.written:
                gh(['secret', 'set', name, '--repo', REPOSITORY], value.encode())
                self.written.add(name)
        require(set(SECRET_NAMES) <= stored_secrets(), 'Secret storage metadata verification failed')
        self.complete = True
        self.app['pem'] = ''
        return {'appId': self.app['id'], 'slug': self.app['slug'], 'repository': REPOSITORY,
                'credentialsStored': list(SECRET_NAMES), 'automaticMaintenanceEnabled': False}


class SetupServer(HTTPServer):
    def handle_error(self, request, client_address):
        print('Setup request failed; credential details suppressed', flush=True)


def serve():
    require(not (stored_secrets() & set(SECRET_NAMES)), 'Release App secrets already exist; do not register another App')
    require(json.loads(gh(['api', 'user']))['login'] == 'joshyorko', 'Bootstrap CLI must be authenticated as joshyorko')
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass  # Requests contain one-use callback codes/state.

        def respond(self, body, status=200, location=None):
            self.send_response(status)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Cache-Control', 'no-store')
            self.send_header('Referrer-Policy', 'no-referrer')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.send_header('Content-Security-Policy', "default-src 'none'; form-action https://github.com; base-uri 'none'; frame-ancestors 'none'")
            if location:
                self.send_header('Location', location)
            self.end_headers()
            self.wfile.write(body.encode())

        def do_GET(self):
            try:
                require(self.headers.get('Host') == host, 'Unexpected callback host')
                url = urllib.parse.urlsplit(self.path)
                query = urllib.parse.parse_qs(url.query)
                if url.path == '/':
                    if registration.app:
                        link = 'https://github.com/apps/' + registration.app['slug'] + '/installations/new'
                        return self.respond('<p>App already registered. Finish installation on ONLY joshyorko/rcc.</p><a href="' + link + '">Install App</a>')
                    manifest = html.escape(json.dumps(registration.manifest()), quote=True)
                    return self.respond('<h1>Dedicated RCC Release Metadata App</h1><p>Sign in to GitHub as joshyorko, create this private App, then choose Only select repositories and rcc. No keys are displayed or downloaded. This does not activate automation or publish.</p><form method="post" action="https://github.com/settings/apps/new?state=' + registration.state + '"><input type="hidden" name="manifest" value="' + manifest + '"><button>Create dedicated App on GitHub</button></form>')
                if url.path == '/registered':
                    return self.respond('Continue to GitHub installation.', 303, registration.register(query))
                if url.path == '/installed':
                    result = registration.install(query)
                    print(json.dumps(result), flush=True)
                    return self.respond('<h1>RCC App credentials stored</h1><p>Repository scope verified: ONLY joshyorko/rcc. Automatic maintenance is still off. Return to the agent for exact-head CI qualification.</p>')
                return self.respond('Not found', 404)
            except RuntimeError as error:
                self.respond(html.escape(str(error)), 400)
            except Exception:
                self.respond('Setup failed; no credential details logged. Inspect public setup status only.', 400)

    server = SetupServer(('127.0.0.1', 0), Handler)
    server.timeout = 1
    host = f'127.0.0.1:{server.server_port}'
    registration = Registration('http://' + host)
    print('Setup page: http://' + host + '/', flush=True)
    deadline = time.monotonic() + 1200
    try:
        while not registration.complete and time.monotonic() < deadline:
            server.handle_request()
        require(registration.complete, 'Setup expired after 20 minutes; no automatic activation performed')
    finally:
        if registration.app:
            registration.app['pem'] = ''
        server.server_close()


if __name__ == '__main__':
    serve()

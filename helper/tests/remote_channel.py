"""Exercise HTTPS repodata and package downloads with a trusted local CA."""
import hashlib
import http.server
import io
import json
import os
import platform
import ssl
import subprocess
import sys
import tarfile
import tempfile
import threading
from pathlib import Path


def main(binary):
    target = {("Linux", "x86_64"): "linux-64", ("Linux", "aarch64"): "linux-aarch64",
              ("Darwin", "arm64"): "osx-arm64", ("Darwin", "x86_64"): "osx-64",
              ("Windows", "AMD64"): "win-64"}[platform.system(), platform.machine()]
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        cert, key = root / "cert.pem", root / "key.pem"
        ca_key, ca = root / "ca-key.pem", root / "ca.pem"
        csr, extensions = root / "server.csr", root / "server.ext"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(ca_key),
                        "-out", str(ca), "-days", "1", "-subj", "/CN=RCC test CA",
                        "-addext", "basicConstraints=critical,CA:TRUE", "-addext", "keyUsage=critical,keyCertSign,cRLSign"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run(["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-keyout", str(key),
                        "-out", str(csr), "-subj", "/CN=localhost"],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        extensions.write_text("subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\nbasicConstraints=CA:FALSE\n")
        subprocess.run(["openssl", "x509", "-req", "-in", str(csr), "-CA", str(ca), "-CAkey", str(ca_key),
                        "-CAcreateserial", "-out", str(cert), "-days", "1", "-extfile", str(extensions)],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        marker = "share/rcc-https-receipt"
        record = dict(name="https-receipt", version="1", build="0", build_number=0,
                      depends=[], subdir=target)
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w:bz2") as tar:
            for name, body in [(marker, b"https installed\n"), ("info/files", marker.encode()+b"\n"),
                               ("info/index.json", json.dumps(record).encode())]:
                item = tarfile.TarInfo(name); item.size = len(body)
                tar.addfile(item, io.BytesIO(body))
        payload = archive.getvalue()
        record.update(size=len(payload), sha256=hashlib.sha256(payload).hexdigest())
        served = {f"/{target}/repodata.json": json.dumps({"info": {"subdir": target},
                  "packages": {"https-receipt-1-0.tar.bz2": record}, "repodata_version": 1}).encode(),
                  "/noarch/repodata.json": json.dumps({"info": {"subdir": "noarch"},
                  "packages": {}, "repodata_version": 1}).encode(),
                  f"/{target}/https-receipt-1-0.tar.bz2": payload}
        requests = []
        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                body = served.get(self.path)
                self.send_response(200 if body is not None else 404)
                if body is not None:
                    self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                if body is not None: self.wfile.write(body)
            def log_message(self, *_): pass
        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert, key)
        server.socket = context.wrap_socket(server.socket, server_side=True)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        try:
            channel = f"https://localhost:{server.server_port}"
            base = dict(protocol=1, prefix=str(root/"prefix"), cache=str(root/"cache"),
                        platform=target, channels=[channel], packages=["https-receipt=1"])
            trusted = dict(base, network={"allow_https": True, "ca_bundle_pem": ca.read_text()})
            result = subprocess.run([str(binary)], input=json.dumps(trusted), text=True, capture_output=True)
            assert result.returncode == 0, result.stderr
            assert (root/"prefix"/marker).read_bytes() == b"https installed\n"
            assert any(path.endswith("repodata.json") for path in requests), requests
            assert f"/{target}/https-receipt-1-0.tar.bz2" in requests, requests
            rejected = dict(base, prefix=str(root/"untrusted-prefix"), cache=str(root/"untrusted-cache"),
                            network={"allow_https": True, "ca_bundle_pem": ""})
            failure = subprocess.run([str(binary)], input=json.dumps(rejected), text=True, capture_output=True)
            assert failure.returncode != 0, "untrusted certificate unexpectedly succeeded"
            print(json.dumps({"trusted_install": True, "untrusted_ca_rejected": True,
                              "requests": requests, "platform": target,
                              "helper_sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}))
        finally:
            server.shutdown(); thread.join(); server.server_close()


if __name__ == "__main__":
    main(Path(sys.argv[1]).resolve(strict=True))

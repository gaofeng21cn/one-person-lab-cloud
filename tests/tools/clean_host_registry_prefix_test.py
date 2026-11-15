import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CLEAN_HOST = ROOT / 'tools/local-v2-clean-host.ts'
COMPOSE = ROOT / 'deploy/portable/compose.local-v2.yaml'

CLOUD_IMAGE = 'registry.example/cloud@sha256:' + 'a' * 64
WORKSPACE_IMAGE = 'registry.example/workspace@sha256:' + 'b' * 64
SOURCE_SHA = 'c' * 40


class CleanHostRegistryPrefix(unittest.TestCase):
    """The Build owner's approved prefix must cover the User's OCI destination.

    The Build owner resolves the destination of a User's own registry from the
    User external binding and refuses any destination whose registry host or
    namespace is outside its approved prefix. The installation declares both:
    the registry namespace the User is bound to, and the prefix the Build owner
    may write into. Those are one installation fact. When the two drift, every
    Build is refused with "user OCI destination is outside the approved
    installation" before a single byte is built.
    """

    def setUp(self):
        self.directory = Path(tempfile.mkdtemp(prefix='opl-clean-host-registry-'))
        self.addCleanup(shutil.rmtree, self.directory, ignore_errors=True)

    def installation_env(self):
        completed = subprocess.run(
            ['node', str(CLEAN_HOST),
             '--product-root', str(ROOT),
             '--directory', str(self.directory),
             '--project', 'opl-test-registry-1',
             '--source-sha', SOURCE_SHA,
             '--cloud-image', CLOUD_IMAGE,
             '--workspace-image', WORKSPACE_IMAGE,
             '--storage-root', '/var/lib/opl-quota/storage'],
            cwd=str(ROOT), capture_output=True, text=True,
            env={**os.environ, 'NODE_OPTIONS': '--experimental-strip-types'},
        )
        self.assertEqual(completed.returncode, 0,
                         f'the clean-host facts must be produced: {completed.stderr}')
        env_file = self.directory / 'installation.env'
        self.assertTrue(env_file.is_file(), 'the installation facts must be written')
        values = {}
        for line in env_file.read_text().splitlines():
            if re.match(r'^[A-Za-z_][A-Za-z0-9_]*=', line):
                name, _, value = line.partition('=')
                values[name] = value
        return values

    def test_build_prefix_covers_the_users_oci_destination(self):
        values = self.installation_env()
        host = values.get('OPL_WORKSPACE_REGISTRY_HOST', '')
        namespace = values.get('OPL_WORKSPACE_REGISTRY_NAMESPACE', '')
        prefix = values.get('OPL_BUILD_REGISTRY_PREFIX', '')
        self.assertTrue(host and namespace, 'the installation must declare its registry host and namespace')
        self.assertEqual(prefix, f'{host}/{namespace}',
                         'the Build owner prefix must name the registry namespace the User is bound to')

    def test_published_topology_derives_the_same_prefix(self):
        """The published topology and the qualification facts must agree.

        The published compose derives the Build owner's prefix from the same two
        facts by default. The qualification facts may only narrow it to the same
        registry, never to a different namespace.
        """
        compose = COMPOSE.read_text()
        self.assertRegex(compose, r'OPL_BUILD_REGISTRY_PREFIX:.*OPL_WORKSPACE_REGISTRY_NAMESPACE',
                         'the published topology must derive the Build prefix from the workspace registry namespace')
        values = self.installation_env()
        prefix = values.get('OPL_BUILD_REGISTRY_PREFIX', '')
        namespace = values.get('OPL_WORKSPACE_REGISTRY_NAMESPACE', '')
        self.assertIn(namespace, prefix,
                      'the Build prefix must stay inside the namespace the User is bound to')


if __name__ == '__main__':
    unittest.main()

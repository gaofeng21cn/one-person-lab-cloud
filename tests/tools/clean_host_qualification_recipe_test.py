import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / '.github/workflows/clean-host-qualification.yml'
INSTALLER = ROOT / 'deploy/portable/install.mjs'
ENV_EXAMPLE = ROOT / 'deploy/portable/opl-cloud.env.example'
CLEAN_HOST = ROOT / 'tools/local-v2-clean-host.ts'


class CleanHostQualificationPackageSchemaDigest(unittest.TestCase):
    """The qualification and the installer must verify one package schema.

    The qualification downloads the package schema and checks it against the
    digest the installation declares. The installer verifies the same file
    against a constant it carries. The declaration and the constant are one
    fact: when they drift, the qualification either checks bytes the installer
    would refuse, or stops checking anything at all.
    """

    def setUp(self):
        self.workflow = WORKFLOW.read_text()
        self.installer = INSTALLER.read_text()
        self.env_example = ENV_EXAMPLE.read_text()
        self.clean_host = CLEAN_HOST.read_text()

    def test_installer_carries_the_package_schema_digest(self):
        digests = set(re.findall(r"PACKAGE_SCHEMA_DIGEST\s*=\s*'sha256:[0-9a-f]{64}'", self.installer))
        self.assertEqual(len(digests), 1, f'the installer must carry one package schema digest, found {digests}')

    def test_installation_facts_state_the_installers_digest(self):
        installer = re.search(r"PACKAGE_SCHEMA_DIGEST\s*=\s*'(sha256:[0-9a-f]{64})'", self.installer).group(1)
        facts = set(re.findall(r'OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST:\s*"(sha256:[0-9a-f]{64})"', self.clean_host))
        self.assertEqual(facts, {installer}, 'the installation facts must state the digest the installer verifies')

    def test_qualification_reads_the_digest_the_installation_declares(self):
        """The qualification checks the digest the installation states.

        It used to read the env example, which states a placeholder for every
        value, so the check was handed a placeholder and stopped before the
        qualification could run.
        """
        self.assertIn('"$directory/installation.env"', self.workflow,
                      'the qualification must read the package schema digest from the installation it declared')
        self.assertNotIn('deploy/portable/opl-cloud.env.example', self.workflow,
                         'the qualification must not read the package schema digest from the env example')

    def test_env_example_keeps_its_placeholder(self):
        """The env example states placeholders for every value.

        The example stays a template; the digest lives in the installer that
        enforces it and in the installation facts that declare it.
        """
        self.assertRegex(self.env_example, r'(?m)^OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST=<.+>$')

    def test_qualification_checks_the_digest_it_read(self):
        self.assertIn('sha256sum --check', self.workflow,
                      'the qualification must still verify the downloaded schema against the digest')


if __name__ == '__main__':
    unittest.main()

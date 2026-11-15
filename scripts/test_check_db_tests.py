import unittest
import pathlib
import tempfile
from check_db_tests import discover_required_tests, verify

class EvidenceTests(unittest.TestCase):
    required = [{"package": "backend/repository", "test": "TestMoney"}]
    def events(self, action="pass", name="TestMoney"):
        return [{"Package": "backend/repository", "Test": name, "Action": action},
                {"Package": "backend/repository", "Action": "pass"}]
    def test_pass(self):
        self.assertFalse(verify(self.events(), self.required)[1])
    def test_package_without_tests_is_allowed(self):
        events = self.events() + [{"Package": "backend/dto", "Action": "skip"}]
        self.assertFalse(verify(events, self.required)[1])
    def test_missing(self):
        self.assertTrue(verify([], self.required)[1])
    def test_skip(self):
        self.assertTrue(verify(self.events("skip"), self.required)[1])
    def test_failure(self):
        self.assertTrue(verify(self.events("fail"), self.required)[1])
    def test_skipped_child(self):
        self.assertTrue(verify(self.events()+self.events("skip", "TestMoney/child"), self.required)[1])
    def test_incomplete_package(self):
        self.assertTrue(verify(self.events()[:1], self.required)[1])
    def test_discovers_database_tests_outside_internal_packages(self):
        with tempfile.TemporaryDirectory() as root:
            backend = pathlib.Path(root)
            for package in ["internal/repository", "routes"]:
                directory = backend / package
                directory.mkdir(parents=True)
                (directory / "admin_integration_test.go").write_text("func TestAdminAccess(t *testing.T) {}\n")
                (directory / "admin_test.go").write_text("func TestUnitOnly(t *testing.T) {}\n")
            self.assertEqual(discover_required_tests(backend), {
                ("backend/internal/repository", "TestAdminAccess"),
                ("backend/routes", "TestAdminAccess"),
            })

if __name__ == "__main__":
    unittest.main()

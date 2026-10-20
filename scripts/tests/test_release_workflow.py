import ast
from pathlib import Path
import unittest

from deployment_fixture import PROJECT


class ReleaseWorkflowTests(unittest.TestCase):
    def test_tag_release_requires_the_same_quality_workflow(self):
        ci = (PROJECT / ".github/workflows/ci.yml").read_text()
        release = (PROJECT / ".github/workflows/release.yml").read_text()
        self.assertIn("workflow_call:", ci)
        self.assertIn("concurrency:", ci)
        self.assertIn("cancel-in-progress: ${{ github.ref_type == 'branch' }}", ci)
        self.assertIn("uses: ./.github/workflows/ci.yml", release)
        self.assertIn("needs: quality", release)
        self.assertIn("scripts/check.sh backend-static", ci)
        self.assertIn("scripts/check.sh backend-tests", ci)
        self.assertIn('scripts/check.sh backend-race "${{ matrix.shard }}"', ci)
        self.assertIn("scripts/check.sh backend-privileged", ci)
        self.assertIn("scripts/check.sh frontend", ci)
        self.assertIn("timeout-minutes:", ci)
        self.assertIn("runs-on: ubuntu-24.04", ci)
        self.assertNotIn("runs-on: ubuntu-latest", ci)
        self.assertNotIn("-race -shuffle=on -p 2 -timeout 20m ./...", ci)
        # 主题构建脚本必须接入门禁：打包/校验失败或分发资产漂移都要挡住发布。
        self.assertIn("scripts/check.sh themes", ci)

    def test_scheduled_deep_race_retains_exhaustive_backend_coverage(self):
        deep_race = (PROJECT / ".github/workflows/deep-race.yml").read_text()
        self.assertIn("schedule:", deep_race)
        self.assertIn("workflow_dispatch:", deep_race)
        self.assertIn("runs-on: ubuntu-24.04", deep_race)
        self.assertIn("go test -race -shuffle=on -p=2 -parallel=2 -timeout=25m -count=1 ./...", deep_race)

    def test_artifacts_and_signature_are_prepared_before_publication(self):
        release = (PROJECT / ".github/workflows/release.yml").read_text()
        self.assertIn("--skip=publish", release)
        self.assertIn("scripts/prepare-release.sh dist", release)
        self.assertLess(release.index("scripts/prepare-release.sh dist"), release.index("softprops/action-gh-release"))
        self.assertIn("fail_on_unmatched_files: true", release)
        self.assertIn("draft: true", release)
        self.assertLess(release.index("softprops/action-gh-release"), release.index("gh release edit"))


class ReleasePackagingTests(unittest.TestCase):
    published = {"beat-server": "./cmd/server", "beat-agent": "./cmd/agent",
                 "beat-cli": "./cmd/beat-cli", "beat-theme": "./cmd/theme"}

    def test_goreleaser_ships_every_published_binary(self):
        config = (PROJECT / ".goreleaser.yaml").read_text()
        for binary, main in self.published.items():
            self.assertIn(f"binary: {binary}", config)
            self.assertIn(f"main: {main}", config)
        self.assertIn("ids: [" + ", ".join(self.published) + "]", config)

    def test_release_archive_validation_requires_every_published_binary(self):
        tree = ast.parse((PROJECT / "scripts/state_archive.py").read_text())
        release_files = next(ast.literal_eval(node.value) for node in tree.body
                             if isinstance(node, ast.Assign)
                             and any(getattr(target, "id", None) == "RELEASE_FILES" for target in node.targets))
        for binary in self.published:
            self.assertIn(binary, release_files)


if __name__ == "__main__":
    unittest.main()

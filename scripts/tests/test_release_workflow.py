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

    def test_release_is_reproducible_and_reuses_the_frontend_build(self):
        ci = (PROJECT / ".github/workflows/ci.yml").read_text()
        release = (PROJECT / ".github/workflows/release.yml").read_text()
        goreleaser = (PROJECT / ".goreleaser.yaml").read_text()
        self.assertIn("upload_frontend_artifact: true", release)
        self.assertIn("BEAT_FRONTEND_STATIC_DIR", ci)
        self.assertIn("name: beat-frontend-static", ci)
        self.assertIn("name: beat-frontend-static", release)
        self.assertIn("runs-on: ubuntu-24.04", release)
        self.assertNotIn("runs-on: ubuntu-latest", release)
        self.assertIn("version: v2.18.3", release)
        self.assertNotIn("version: latest", release)
        self.assertNotIn("npm run build", release)
        self.assertNotIn("actions/setup-node", release)
        self.assertNotIn("before:", goreleaser)
        self.assertNotIn("npm run build", goreleaser)
        self.assertLess(release.index("actions/download-artifact"), release.index("goreleaser-action"))

    def test_tag_release_builds_three_multi_architecture_container_images(self):
        release = (PROJECT / ".github/workflows/release.yml").read_text()
        self.assertIn('tags: ["v*"]', release)
        self.assertIn("  containers:", release)
        self.assertIn("packages: write", release)
        self.assertIn("id-token: write", release)
        self.assertIn("registry: ghcr.io", release)
        self.assertIn("password: ${{ secrets.GITHUB_TOKEN }}", release)
        self.assertIn("images: ghcr.io/${{ github.repository_owner }}/${{ matrix.package }}", release)
        self.assertIn("push: true", release)
        self.assertIn("platforms: linux/amd64,linux/arm64", release)
        self.assertIn("target: ${{ matrix.target }}", release)
        self.assertIn("package: beat\n", release)
        self.assertIn("package: beat-server\n", release)
        self.assertIn("package: beat-agent\n", release)
        self.assertIn("target: combined", release)
        self.assertIn("target: server", release)
        self.assertIn("target: agent", release)
        self.assertIn("needs: [quality, containers]", release)
        self.assertIn("cosign sign --yes", release)


class ContainerImageTests(unittest.TestCase):
    def test_dockerfile_exposes_combined_server_and_agent_targets(self):
        dockerfile = (PROJECT / "Dockerfile").read_text()
        for target in ("combined", "server", "agent"):
            self.assertIn(f" AS {target}\n", dockerfile)
        self.assertIn("GOOS=\"${TARGETOS}\" GOARCH=\"${TARGETARCH}\"", dockerfile)
        self.assertIn("COPY --from=backend-build --chown=65532:65532 /out/beat-server", dockerfile)
        self.assertIn("COPY --from=backend-build --chown=65532:65532 /out/beat-agent", dockerfile)
        self.assertIn("ENV BEAT_CONTAINER_ROLE=agent", dockerfile)
        self.assertIn('CMD ["--config", "/etc/beat-agent/agent.json"]', dockerfile)

    def test_container_entrypoint_can_select_server_or_agent_role(self):
        entrypoint = (PROJECT / "deploy/container-entrypoint.sh").read_text()
        self.assertIn("BEAT_CONTAINER_ROLE", entrypoint)
        self.assertIn("server|agent", entrypoint)
        self.assertIn('exec /usr/local/bin/beat-server "$@"', entrypoint)
        self.assertIn('exec /usr/local/bin/beat-agent "$@"', entrypoint)

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
        self.assertIn("BEAT_RELEASE_SIGNING_KEY", release)
        self.assertIn("beat-agent-manifest.json", release)
        self.assertIn("beat-agent-manifest.sigstore.json", release)
        self.assertIn("beat-agent_linux_amd64", release)
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

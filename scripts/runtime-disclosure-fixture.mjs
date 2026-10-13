// Fictional private projection for actual package/image controls, never a layer.
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

export const disclosureFixture = "Example & Partners Road 1\n1234 Exampletown";

/** Create an owner-readable, fictional file outside the package build context. */
export function privateDisclosureFixture() {
  const directory = mkdtempSync(path.join(tmpdir(), "runtime-disclosure-"));
  const filename = path.join(directory, "disclosure");
  try {
    writeFileSync(filename, disclosureFixture, { mode: 0o600 });
  } catch (error) {
    rmSync(directory, { recursive: true });
    throw error;
  }
  return { directory, filename, close: () => rmSync(directory, { recursive: true }) };
}

/** Copy a correctly owned fixture into a disposable volume without running a
 * shell, changing the image, loosening file permissions or adding a helper image.
 */
export function containerDisclosureFixture(image, docker) {
  const local = privateDisclosureFixture();
  const volume = `business-disclosure-${randomUUID()}`;
  let carrier;
  try {
    docker("volume", "create", volume);
    carrier = docker(
      "create",
      "--network=none",
      "--mount",
      `type=volume,source=${volume},target=/projection`,
      image,
      "--version",
    );
    const owner =
      process.platform === "darwin" ? ["--uid", "65532", "--gid", "65532"] : ["--owner=65532", "--group=65532"];
    const archive = execFileSync("tar", [...owner, "-cf", "-", "-C", local.directory, "disclosure"], { timeout: 5000 });
    execFileSync("docker", ["cp", "--archive", "-", `${carrier}:/projection`], { input: archive, timeout: 30000 });
    docker("rm", carrier);
    carrier = undefined;
    return {
      mount: ["--mount", `type=volume,source=${volume},target=/projection,readonly`],
      arguments: ["--company-disclosure-file", "/projection/disclosure"],
      close: () => {
        try {
          docker("volume", "rm", volume);
        } finally {
          local.close();
        }
      },
    };
  } catch (error) {
    try {
      if (carrier) docker("rm", carrier);
    } finally {
      try {
        docker("volume", "rm", volume);
      } finally {
        local.close();
      }
    }
    throw error;
  }
}

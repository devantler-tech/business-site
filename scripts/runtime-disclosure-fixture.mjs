// Fictional private projection for actual package/image controls, never a layer.
import { execFileSync } from "node:child_process";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { mkdtempSync, writeFileSync, rmSync, mkdirSync, chmodSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";

export const disclosureFixture = "Example & Partners Road 1\n1234 Exampletown";

/** Read-only numeric listing verifies the actual copied file, never an input-header assumption. */
export function assertDisclosureArchive(archive, uid, gid) {
  const listing = execFileSync("tar", ["--numeric-owner", "-tvf", "-"], {
    input: archive,
    encoding: "utf8",
    timeout: 5000,
    maxBuffer: 65536,
    env: { ...process.env, LC_ALL: "C" },
  })
    .trim()
    .split("\n");
  assert.equal(listing.length, 1, "Expected exactly one copied fixture file.");
  const fields = listing[0].trim().split(/\s+/);
  assert.match(fields[0], /^-[rwx-]{9}$/, "Copied fixture must be regular.");
  assert.equal(fields.at(-1), "disclosure", "Unexpected copied fixture entry.");
  if (process.platform === "darwin") {
    assert.equal(fields[2], String(uid), "Copied fixture owner differs.");
    assert.equal(fields[3], String(gid), "Copied fixture group differs.");
  } else {
    assert.equal(fields[1], `${uid}/${gid}`, "Copied fixture ownership differs.");
  }
}

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
  return containerFixture(image, docker, local, 65532, 65532, ["disclosure"], "--company-disclosure-file");
}

/** Preserve a synthetic Kubernetes-style atomic projection's root/group ownership.
 * This is an actual Docker filesystem control, not a live kubelet/controller proof.
 */
export function containerGroupDisclosureFixture(image, docker, owner = 0, group = 65532, mode = 0o440) {
  const directory = mkdtempSync(path.join(tmpdir(), "runtime-group-disclosure-"));
  const timestamp = path.join(directory, "..timestamp");
  let timestampCreated = false;
  const close = () => {
    try {
      if (timestampCreated) chmodSync(timestamp, 0o700);
    } finally {
      rmSync(directory, { recursive: true });
    }
  };
  try {
    mkdirSync(timestamp, { mode: 0o700 });
    timestampCreated = true;
    writeFileSync(path.join(timestamp, "disclosure"), disclosureFixture, { mode });
    chmodSync(path.join(timestamp, "disclosure"), mode);
    symlinkSync("..timestamp", path.join(directory, "..data"));
    symlinkSync("..data/disclosure", path.join(directory, "disclosure"));
    chmodSync(timestamp, 0o550);
  } catch (error) {
    close();
    throw error;
  }
  return containerFixture(
    image,
    docker,
    { directory, close },
    owner,
    group,
    ["..timestamp", "..data", "disclosure"],
    "--company-disclosure-projection",
  );
}

/** Archive ownership is applied by Docker, never by a privileged host operation. */
function containerFixture(image, docker, local, uid, gid, entries, inputFlag) {
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
      process.platform === "darwin"
        ? ["--uid", String(uid), "--gid", String(gid)]
        : [`--owner=${uid}`, `--group=${gid}`];
    const archive = execFileSync("tar", [...owner, "-cf", "-", "-C", local.directory, ...entries], { timeout: 5000 });
    // For streamed tar, --archive remaps entries to the container user instead
    // of retaining these explicit numeric headers (Moby tarCopyOptions).
    execFileSync("docker", ["cp", "-", `${carrier}:/projection`], { input: archive, timeout: 30000 });
    const leaf = entries.includes("..timestamp") ? "..timestamp/disclosure" : "disclosure";
    const copied = execFileSync("docker", ["cp", `${carrier}:/projection/${leaf}`, "-"], {
      timeout: 30000,
      maxBuffer: 65536,
    });
    assertDisclosureArchive(copied, uid, gid);
    docker("rm", carrier);
    carrier = undefined;
    return {
      mount: ["--mount", `type=volume,source=${volume},target=/projection,readonly`],
      arguments: [inputFlag, "/projection/disclosure"],
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

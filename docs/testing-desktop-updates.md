# Testing desktop updates without publishing a release

Test the updater in the **source app** by having it install an existing, newer
published release. A successful update into a new release only tests the updater
in the old app. It does not establish that the new updater works.

For example, package the candidate fix with temporary version `0.2.7` and let it
install the already published `0.2.8`. The target may contain the old updater bug;
the candidate source app performs this update. Use a disposable installation and
restore it afterward. These temporary artifacts must not be distributed as a fix.

## Build a candidate

Once `updater-test-build.yml` is available on the default branch, dispatch
**Build unpublished updater test app**:

- `source_branch`: a branch in this repository containing the candidate fix.
- `test_version`: a stable version lower than the existing published target.
- `platform`: `macos`, `windows`, or `linux`.

The workflow resolves the branch to a commit, runs its checks, overrides only the
packaged version, and uploads installers with `updater-test-build.json`. The
record identifies the source commit and installer digests; displayed version
alone cannot identify the candidate. Baseline branches without `test:updater`
are supported for reproducing failures, and the record explicitly notes that the
regression suite was absent.

The job uses the existing `release` environment and its approval rules. macOS uses
the production signing and notarization configuration. Windows/Linux use their
existing packaging configuration; this does not add Windows signing. All packaging
commands use `--publish never`, the token has only `contents: read`, and no release,
tag, update manifest or attestation is published by this workflow.

The source installer is installed manually. The **target** is the existing release,
with its original signatures and GitHub attestations. Leave the production update
provider, attestation policy and native signature checks enabled. No test bypass
or synthetic update event is needed for the successful native installation test.

## Prepare the machine

1. Record the current stable target and its artifact digests. Confirm it is newer
   than the temporary source version; repeat this check if another release ships
   while testing.
2. Prefer a dedicated test machine, VM or test account. On a workstation, stop
   Tinfoil Proxy and back up the installed application, its user data, updater cache
   and native updater state before replacing anything. Keep launch-at-login off
   during the test. The current proxy will be unavailable while it is stopped.
3. Install the candidate using the normal platform installer and location. On
   macOS this is the `.pkg` and `/Applications/Tinfoil Proxy.app`; its postinstall
   hook launches the app automatically. On Windows use NSIS as the normal user;
   on Linux use the `.deb` and normal package-manager permissions. Remove the test
   app before reinstalling a lower source version if the installer refuses a
   downgrade, preserving the saved state.
4. Record a baseline from `/verification-document`: `runtime.instanceId` and
   `runtime.software.version`, plus the running application path/PID. Confirm the
   source version and source artifact digest. Save a reset point for each scenario.

Do not run production and test copies with the same application identity at once.
They share single-instance coordination and updater state. Wait for the native
installer to finish before resetting or restoring a snapshot.

## Required native scenarios

Run each scenario from the source reset point, keeping an external observer alive
while the app quits. Use bounded waits and save failures instead of assuming that
a visible dialog or downloaded ZIP means installation succeeded.

| Scenario | Required evidence |
| --- | --- |
| Restart now | Source exits; installer completes; target launches; installed bundle and running proxy report the target version; proxy instance ID changes. |
| Later, then normal Quit | The app keeps serving until Quit; installation completes; reopening runs the target with the expected settings. Do not force-kill the source. |
| Cached update | Repeat with the downloaded target left in the cache; verification, installation and relaunch still complete. |
| Repeated scheduled checks while pending | Only one automatic prompt for that version. Leave the source running through the real six-hour check interval for a native soak; the fast regression suite separately exercises repeated checks. |
| Relaunch source after an installation failure | A remembered notification suppresses the duplicate dialog, but verification and native preparation still happen. Use a failed install that retained the source, or restore captured source/pending state and record that reset method. |
| Verification request blocked or timed out | No ready dialog, native staging or installation. Removing the network fault and checking again succeeds. Use a scoped test proxy/network rule, not a changed verification policy. |
| Quit while verification is pending | No unverified installation. On the next source launch, the cached download can be verified and prepared again. |

Also exercise native staging/installation failure and retry. On Windows check both
per-user installation and an installation that requires elevation. On Linux check
the package-manager authorization path. Failures must not be reported as successful
updates. In-process regression tests cover rejection, timeout-equivalent verifier
errors and native error events; they do not replace these OS installation checks.

## Observe the result from outside the app

On macOS, inspect the installed bundle version after the native installer finishes:

```sh
/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' \
  '/Applications/Tinfoil Proxy.app/Contents/Info.plist'
```

On every platform, fetch the running proxy's verification document using the
configured port (3301 by default):

```sh
curl --fail --silent http://127.0.0.1:3301/verification-document
```

Check `runtime.software.version` against the expected target and compare
`runtime.instanceId` with the source baseline. Check the OS process path as well,
so a different proxy already listening on the port cannot satisfy the test. Make
one ordinary request through the proxy using the test account's normal credentials
and confirm it succeeds; do not put credentials or request content in the report.

Collect the macOS native installer logs under
`~/Library/Caches/sh.tinfoil.proxy.ShipIt/`, including rotated logs. For additional
main-process logs in a separate reproduction, launch the signed app executable
from a terminal and redirect stdout/stderr to a test log. Keep the normal `.pkg`
installation scenario as well because it exercises its actual ownership and
postinstall behavior. Windows/Linux runs should retain installer/package-manager
errors along with the observed process and version changes.

For each scenario, record:

- Source commit, temporary version, platform/OS and source installer SHA-256.
- Target version, target artifact SHA-256 and installation method/ownership.
- User action, timestamps, relevant verification/native installer logs and errors.
- Installed version, running version, changed proxy instance ID and smoke-test result.
- Notification count, settings preservation and the final pass/fail result.

Restore the backed-up application and state after the installer and test processes
have exited. Neither a passing mocked lifecycle test nor an unpublished build is
an end-to-end installation result. Require recorded native results for supported
platforms before distributing an updater change.

# Installation

BatoX runs as a [Batocera service](https://wiki.batocera.org/launch_a_script) on the console and
is installed over SSH. There are three ways to install it:

| Method | Best for |
|---|---|
| [BatoX Installer](#installing-with-the-batox-installer) | Most people: a desktop app, no terminal needed |
| [A release, by hand](#installing-a-release-manually) | People comfortable with `ssh` and `scp` |
| [`make deploy`](#installing-with-make-deploy) | Developers working on BatoX itself |

This guide also covers SSH access, upgrading, uninstalling and troubleshooting.

- [Requirements](#requirements)
- [Installing with the BatoX Installer](#installing-with-the-batox-installer)
- [SSH access to Batocera](#ssh-access-to-batocera)
- [Installing with `make deploy`](#installing-with-make-deploy)
- [Installing a release manually](#installing-a-release-manually)
- [First run](#first-run)
- [Day-to-day commands](#day-to-day-commands)
- [Upgrading](#upgrading)
- [Upgrading from MyMine](#upgrading-from-mymine)
- [Uninstalling](#uninstalling)
- [Troubleshooting](#troubleshooting)

## Requirements

- **Console:** Batocera **v38 or newer** (BatoX relies on the `batocera-services` system) on a
  Raspberry Pi 4/5 or another arm64 board, or on an x86_64 PC.
- **Computer:** Windows, macOS or Linux. With the [BatoX Installer](https://github.com/monteirohat/BatoX_Installer) nothing else is
  needed. For the manual methods you need `ssh` and `scp`, and to build from source also
  [Go 1.24+](https://go.dev/dl) and `make`. The binary has no CGO dependencies, so it
  cross-compiles from Linux, macOS or Windows.

## Installing with the BatoX Installer

The [BatoX Installer](https://github.com/monteirohat/BatoX_Installer) is a desktop app, in the style of balenaEtcher, that does every step
of the installation for you over SSH.

1. Download it for your system from the
   [latest installer release](https://github.com/monteirohat/BatoX_Installer/releases/latest): a setup or portable `.exe` for Windows,
   a `.dmg` for macOS (Apple Silicon or Intel), or an `.AppImage` or `.deb` for Linux.
2. Open it and choose the BatoX **version**. The latest release is selected; choose **Choose**
   to pick another release or type a tag such as `v1.0.0`.
3. Enter the **console** address, `batocera.local` or its IP address, and click
   **Check console**. The installer shows the board, the Batocera version and whether BatoX is
   already installed. It uses the default login, `root` / `linux`; change it under
   **SSH login** if you set another password.
4. Click **Install**, or **Update** if an older BatoX is installed.

At the end it shows the BatoX address and the API token, with buttons to open BatoX and copy
the token. Behind the scenes it follows the same steps as `make deploy`: it downloads the right
binary for the console's processor, verifies it against the release's `SHA256SUMS`, migrates a
MyMine install if there is one, copies the files, starts the service and checks the API. The SSH
password is never saved.

The installer builds are not code-signed yet, so the first launch shows a warning. See the
[installer README](https://github.com/monteirohat/BatoX_Installer#download) for how to open it on each system.

## SSH access to Batocera

The BatoX Installer handles SSH for you; this section is for the manual methods and for
troubleshooting.

Batocera has SSH enabled by default:

| | |
|---|---|
| User | `root` |
| Default password | `linux` |
| Address | `batocera.local`, or the console's IP address |

The IP address is shown on the console under **MAIN MENU > NETWORK SETTINGS**. Use it whenever
`batocera.local` does not resolve on your network.

### Testing the connection

```bash
ssh root@batocera.local
```

On the first connection SSH asks whether you trust the host: answer `yes`. Then type the password
`linux`. Use `exit` to leave the console.

### Setting up key-based login (recommended)

Without an SSH key, every deploy asks for the password. To log in with a key instead:

1. Create a key on your computer. You only need to do this once; press Enter at every prompt:

   ```bash
   ssh-keygen -t ed25519
   ```

2. Copy the key to Batocera. You will type the password `linux` one last time:

   ```bash
   ssh-copy-id root@batocera.local
   ```

3. Check that it worked. This should log in without asking for a password:

   ```bash
   ssh root@batocera.local
   ```

## Installing with `make deploy`

From a clone of the repository:

```bash
make deploy
```

Options, set as environment variables:

| Variable | Default | Meaning |
|---|---|---|
| `BATOCERA_HOST` | `batocera.local` | Host name or IP of the console |
| `BATOX_ARCH` | `arm64` | `arm64` for Raspberry Pi and other ARM boards, `amd64` for PCs |

```bash
BATOCERA_HOST=192.168.1.50 make deploy
BATOX_ARCH=amd64 make deploy
```

The deploy script:

1. builds `build/batox-linux-<arch>`;
2. migrates an existing MyMine install, if there is one (see below);
3. copies the binary to `/userdata/system/batox/batox` and the service script to
   `/userdata/system/services/batox`;
4. creates `/userdata/system/batox/config.json` from `config.example.json` if it does not exist
   (an existing configuration is never overwritten);
5. enables and starts the service, calls `/api/v1/ping`, and prints the API token.

The service also appears on the console under **MAIN MENU > SYSTEM SETTINGS > SERVICES**, where
it can be turned on or off.

## Installing a release manually

Each [release](https://github.com/monteirohat/batox/releases) contains:

| File | Purpose |
|---|---|
| `batox-linux-arm64` | Binary for Raspberry Pi and other arm64 boards |
| `batox-linux-amd64` | Binary for x86_64 PCs |
| `batox` | Service script for `/userdata/system/services/` |
| `config.example.json` | Optional starting point for `config.json` |
| `SHA256SUMS` | Checksums of the files above |

```bash
HOST=batocera.local
ARCH=arm64   # or amd64

ssh root@$HOST "mkdir -p /userdata/system/batox /userdata/system/services"
scp batox-linux-$ARCH root@$HOST:/userdata/system/batox/batox
scp batox root@$HOST:/userdata/system/services/batox
ssh root@$HOST "chmod +x /userdata/system/batox/batox /userdata/system/services/batox \
  && batocera-services enable batox && batocera-services start batox"
```

The service script must keep the name `batox`, with no `.sh` extension, or Batocera will not
recognize it as a service.

## First run

On its first start BatoX:

- creates `/userdata/system/batox/` with the SQLite catalog (`batox.db`), the logs and the token;
- generates a random API token in `/userdata/system/batox/token`;
- scans the ROM folders and builds the game catalog (with `syncOnStart`, on by default).

Open **http://batocera.local:8080**, paste the token on the **Access** screen and click
**Save and test**. To read the token later:

```bash
./scripts/deploy.sh token
# or, on the console:
cat /userdata/system/batox/token
```

## Day-to-day commands

All of them accept `BATOCERA_HOST`:

```bash
./scripts/deploy.sh status   # is the service running?
./scripts/deploy.sh logs     # last lines of the logs
./scripts/deploy.sh token    # print the API token
./scripts/deploy.sh remove   # remove the service and binary, keep the data
```

On the console itself:

```bash
batocera-services start batox
batocera-services stop batox
/userdata/system/services/batox status
```

## Upgrading

Open the [BatoX Installer](https://github.com/monteirohat/BatoX_Installer), choose the new version and click **Update**. You can also run
`make deploy` again, or copy the new release binary over the old one. The configuration, token,
catalog and logs in `/userdata/system/batox/` are kept.

Because the web app is embedded in the binary, the browser picks up the new interface on the next
page load. If it does not, force a reload with **Ctrl+Shift+R**.

## Upgrading from MyMine

BatoX was previously called MyMine and lived in `/userdata/system/mymine`. The BatoX Installer
and the deploy script both migrate it automatically:

1. stops and disables the `mymine` service and removes its service script;
2. if `/userdata/system/batox` does not exist yet, moves `/userdata/system/mymine` there and
   renames `mymine.db` to `batox.db`, keeping the token, catalog and configuration;
3. installs and starts BatoX as usual.

In the browser, the token and preferences saved by MyMine are copied over the first time the
BatoX page opens, so you don't need to type the token again.

If you install a release by hand instead, do the same yourself:

```bash
ssh root@batocera.local
batocera-services stop mymine; batocera-services disable mymine
rm /userdata/system/services/mymine
mv /userdata/system/mymine /userdata/system/batox
mv /userdata/system/batox/mymine.db /userdata/system/batox/batox.db
rm -f /userdata/system/batox/mymine
```

## Uninstalling

```bash
./scripts/deploy.sh remove
```

This stops and disables the service and removes the binary and the service script. Your data
(configuration, token, catalog and logs) stays in `/userdata/system/batox/`; delete that folder
to remove everything:

```bash
ssh root@batocera.local "rm -rf /userdata/system/batox"
```

BatoX never touches your games, music or themes when it is removed.

## Troubleshooting

**`ssh-copy-id: ERROR: No identities found`**
There is no SSH key on your computer yet. Run `ssh-keygen -t ed25519` first.

**`Could not resolve hostname batocera.local`**
Your network does not resolve the name. Use the console's IP address instead, for example
`BATOCERA_HOST=192.168.1.50 make deploy`.

**`WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!`**
This happens after reinstalling Batocera, because the console gets a new identity. Remove the old
entry and connect again (use the IP instead of the name if that is what you use):

```bash
ssh-keygen -R batocera.local
```

After a reinstall you also need to run `ssh-copy-id` again.

**`Connection refused` or `Connection timed out`**
Check that the console is on, on the same network, and that the address is right.

**The BatoX Installer can't find or connect to the console**
The installer shows the reason on screen. Most often the address is wrong (try the IP address
instead of `batocera.local`), the console is off or on another network, or the SSH password was
changed (set it under **SSH login**). The SSH problems below apply to the installer too.

**The deploy says "The API did not answer"**
Look at the logs with `./scripts/deploy.sh logs`. The most common cause is another program using
port 8080; in that case set a different port with `"listen": ":8090"` in
`/userdata/system/batox/config.json` and start the service again.

**The page asks for the token again**
The token is stored per browser. Read it with `./scripts/deploy.sh token`.

**EmulationStation controls fail (start, stop, restart)**
BatoX uses `/etc/init.d/S31emulationstation` by default. Check the script name on your console
with `ls /etc/init.d/ | grep -i emulation` and set `esInitScript` in `config.json` if it differs.
See [Configuration](configuration.md).

**A new song does not play**
EmulationStation picks background music when it builds its playlist. Restart it from the
**EmulationStation** screen of the web app, and check that background music is enabled in
EmulationStation's sound settings.

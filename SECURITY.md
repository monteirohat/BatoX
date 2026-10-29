# Security policy

## Supported versions

Security fixes are made for the latest release only.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/monteirohat/batox/security/advisories/new),
not in public issues. Include the steps to reproduce and the BatoX and Batocera versions. You
should get a first answer within a week.

## Security model

BatoX is designed for a **trusted home network**. Keep this in mind when deploying it:

- **It runs as root.** Like every Batocera service, BatoX runs as `root` and can delete files,
  change system settings and reboot or power off the console.
- **Access is protected by a single token.** Every API route except `/api/v1/ping` and
  `/api/v1/about` requires it. The token is generated randomly on first run and stored in
  `/userdata/system/batox/token` with mode `0600`. Anyone with the token has full access.
- **Traffic is plain HTTP.** The token travels unencrypted. Do not expose port 8080 to the
  internet or to untrusted networks. For remote access, use a VPN (for example WireGuard or
  Tailscale) instead of port forwarding.
- **The web app stores the token in the browser's `localStorage`.** Don't save it on shared
  computers.
- **Sensitive settings are readable.** `GET /api/v1/batocera/settings` returns every
  `batocera.conf` value, including Wi-Fi passwords. The web app masks them, but the API returns
  them to any client holding the token.

## Hardening built in

- No endpoint runs arbitrary commands; system tools are called with fixed arguments and without a
  shell.
- File names from clients are reduced to their base name, and hidden names are rejected.
- Theme ZIPs are checked for entries escaping the destination ("zip slip") and for symlinks, and
  the extracted size is capped.
- Music paths are resolved and checked to stay inside the music folder.
- `batocera.conf` keys are validated so they can't be read as command-line options or break the
  file.
- Uploads are size-limited and streamed to disk.

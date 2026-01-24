# Windows (WSL2)

Nexus on Windows is recommended **via WSL2** (Ubuntu recommended). The gateway
and CLI run inside Linux for a consistent runtime. Native Windows installs are
untested and more likely to break (service install is not supported on Windows).

## Install (WSL2)
- Follow the Linux setup inside WSL2: build the `nexus` binary and run `nexus serve`.
- Optional: install the user service via `nexus service install` (requires systemd).

### 1) Install WSL2 + Ubuntu

Open PowerShell (Admin):

```powershell
wsl --install
# Or pick a distro explicitly:
wsl --list --online
wsl --install -d Ubuntu-24.04
```

Reboot if prompted.

### 2) Enable systemd (required for `nexus service install`)

In your WSL terminal:

```bash
sudo tee /etc/wsl.conf >/dev/null <<'EOF'
[boot]
systemd=true
EOF
```

Then from PowerShell:

```powershell
wsl --shutdown
```

Re-open Ubuntu, then verify:

```bash
systemctl --user status
```

### 3) Install Nexus (inside WSL)

Follow the Linux deployment steps:

```bash
git clone https://github.com/haasonsaas/nexus.git
cd nexus
go mod download
go build -o bin/nexus ./cmd/nexus
cp nexus.example.yaml nexus.yaml
./bin/nexus migrate up
./bin/nexus serve
```

Optional service install:

```bash
./bin/nexus service install
```

## Expose WSL services over LAN (portproxy)

WSL has its own virtual network. If another machine needs to reach Nexus (HTTP,
gRPC, Canvas host), forward a Windows port to the current WSL IP.

Example (PowerShell **as Administrator**):

```powershell
$Distro = "Ubuntu-24.04"
$ListenPort = 8080
$TargetPort = 8080

$WslIp = (wsl -d $Distro -- hostname -I).Trim().Split(" ")[0]
if (-not $WslIp) { throw "WSL IP not found." }

netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=$ListenPort `
  connectaddress=$WslIp connectport=$TargetPort
```

Allow the port through Windows Firewall (one-time):

```powershell
New-NetFirewallRule -DisplayName "WSL Nexus $ListenPort" -Direction Inbound `
  -Protocol TCP -LocalPort $ListenPort -Action Allow
```

Refresh the portproxy after WSL restarts:

```powershell
netsh interface portproxy delete v4tov4 listenport=$ListenPort listenaddress=0.0.0.0 | Out-Null
netsh interface portproxy add v4tov4 listenport=$ListenPort listenaddress=0.0.0.0 `
  connectaddress=$WslIp connectport=$TargetPort | Out-Null
```

Notes:
- Use the **Windows host IP** for remote clients (not `127.0.0.1`).
- If you expose the gateway, ensure `auth.jwt_secret` or API keys are set.
- Repeat the portproxy for any additional ports (gRPC, canvas_host).


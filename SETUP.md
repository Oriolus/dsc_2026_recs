# Running the Workshop Stand in Docker (macOS + Colima, Windows)

The stand is four containers built from one image: `candidates-retrieval`,
`feature-store`, `orchestrator` and `toolbox`. You need Docker Engine,
Docker Compose v2, `make` and internet access during the first build.

**Do the first build before the workshop.** It downloads the Go image and
compiles `buf`, `ghz`, `grpcurl`, `benchstat` and the protobuf plugins: it takes
5-15 minutes and ~3 GB of disk. Conference Wi-Fi is not the place for it.

Resources for the Docker VM: **4 CPUs and 4 GB RAM** (3 GB is the minimum).
CPUs matter more than memory: load tests run at 237 rps, and with fewer CPUs
latency numbers get noisy. See [Memory budget](#memory-budget) for the measurements.

Network: the build talks only to Docker Hub, `deb.debian.org`,
`proxy.golang.org` and `sum.golang.org` (the Dockerfile pins the public Go
proxy on purpose). If you are behind a corporate HTTP proxy, configure it for
the Docker daemon / VM (see below), not in the Dockerfile.

---

## macOS with Colima

Works on Apple Silicon and Intel. Images are multi-arch, no Rosetta needed.

### 1. Install

```sh
brew install colima docker docker-compose docker-buildx make
```

### 2. Make `docker compose` and `docker buildx` visible to the Docker CLI

Homebrew installs the plugins into a directory the Docker CLI does not look into.
Add `cliPluginsExtraDirs` to `~/.docker/config.json` (create the file if needed,
keep the existing keys):

```json
{
  "auths": {},
  "cliPluginsExtraDirs": [
    "/opt/homebrew/lib/docker/cli-plugins"
  ]
}
```

On Intel Macs the path is `/usr/local/lib/docker/cli-plugins`. Check:

```sh
docker compose version   # Docker Compose version v2.x
docker buildx version
```

If you skip this step, `make` falls back to the standalone `docker-compose`
binary, which also works.

### 3. Start the VM

```sh
colima start --cpu 4 --memory 4 --disk 40 --vm-type vz --mount-type virtiofs
```

* `--vm-type vz --mount-type virtiofs` (macOS 13+) is much faster than the default QEMU/sshfs.
  On macOS 12 drop both flags.
* The settings are saved in the `default` profile: next time `colima start` is enough.
  To change resources later: `colima stop && colima start --cpu 6 --memory 6`.
* Check: `colima status` and `docker info | grep -E 'CPUs|Total Memory'`.

### 4. Clone the repo under your home directory

Colima mounts only `$HOME` into the VM (writable). The `./profiles` bind mount
fails silently if the project lives elsewhere, e.g. in `/opt` or on an external drive.

```sh
cd ~/projects && git clone <repo-url> recsys-workshop && cd recsys-workshop
```

### 5. Build, start, verify

```sh
make up      # first run builds the image
make check   # health of all services + smoke call
make test-v1-small
```

Ports are forwarded to `localhost` automatically: http://localhost:6060/debug/pprof/,
pprof UI at http://localhost:8080 / http://localhost:8081 after `make pprof-ui ...`.

### Corporate HTTP proxy (Colima)

```sh
colima start --env HTTP_PROXY=http://proxy:3128 --env HTTPS_PROXY=http://proxy:3128 \
  --env NO_PROXY=localhost,127.0.0.1
```

and pass the same variables to the build:
`docker compose build --build-arg HTTP_PROXY=... --build-arg HTTPS_PROXY=...`
(`HTTP_PROXY`/`HTTPS_PROXY` are predefined build args, no Dockerfile change needed).

### Troubleshooting (macOS)

| Symptom | Fix |
|---|---|
| `Cannot connect to the Docker daemon at unix:///var/run/docker.sock` | `colima start`; check `docker context ls` points to `colima` (`docker context use colima`) |
| `docker: unknown command: docker compose` | step 2, or just use `make` (it falls back to `docker-compose`) |
| `compose build requires buildx` / legacy builder warning | `brew install docker-buildx` + step 2 |
| `profiles/` stays empty after `make profile-*` | the repo is outside `$HOME`; move it or `colima start --mount /path/to/dir:w` |
| Build killed / `signal: killed` while compiling tools | not enough VM memory (Colima defaults to 2 GB): `colima stop && colima start --memory 4` |
| `no space left on device` | `docker system prune -a`, or recreate the VM with a bigger `--disk` |
| Ports already in use (`50050`, `6060`, `8080`) | another process holds them: `lsof -i :8080` |

---

## Windows (WSL 2)

Run everything **inside a WSL 2 Linux distribution**: the Makefile and scripts are
POSIX shell. PowerShell-only setups are covered at the end.

### 1. Install WSL 2 and Ubuntu

In PowerShell as Administrator:

```powershell
wsl --install -d Ubuntu-24.04
```

Reboot, open "Ubuntu" from the Start menu and create a user.

Give WSL enough resources: create `%UserProfile%\.wslconfig`:

```ini
[wsl2]
memory=4GB
processors=4
```

then `wsl --shutdown` in PowerShell and reopen Ubuntu.

### 2. Install Docker

Choose one:

* **Docker Desktop** (simplest; note: requires a paid subscription in companies
  with >250 employees or >$10M revenue). Settings → General → *Use the WSL 2 based engine*;
  Settings → Resources → WSL Integration → enable *Ubuntu-24.04*.
* **Docker Engine directly in WSL** (free, no Desktop). Inside Ubuntu:

  ```sh
  curl -fsSL https://get.docker.com | sudo sh
  sudo usermod -aG docker $USER
  ```

  Close and reopen the Ubuntu window. If `systemctl` is not available, enable systemd:
  add `[boot]` / `systemd=true` to `/etc/wsl.conf`, then `wsl --shutdown`.
* **Rancher Desktop** with the *dockerd (moby)* engine also works (enable WSL integration).

Then inside Ubuntu:

```sh
sudo apt-get update && sudo apt-get install -y make git
docker compose version
```

### 3. Clone the repo inside the Linux filesystem

```sh
mkdir -p ~/projects && cd ~/projects
git clone <repo-url> recsys-workshop && cd recsys-workshop
```

Do **not** work from `/mnt/c/...`: bind mounts and builds are many times slower,
and Windows checkouts may get CRLF line endings. (The repo ships a `.gitattributes`
that forces LF, and the image re-applies the executable bit to scripts, but the
Linux filesystem is still the right place.) To open the project in an IDE, use
`\\wsl$\Ubuntu-24.04\home\<user>\projects\recsys-workshop`, or VS Code / GoLand
WSL remote support.

### 4. Build, start, verify

```sh
make up
make check
make test-v1-small
```

`localhost` ports are forwarded to Windows: open http://localhost:6060/debug/pprof/
and the pprof UI (http://localhost:8080) in your Windows browser.

### Troubleshooting (Windows)

| Symptom | Fix |
|---|---|
| `bind: An attempt was made to access a socket in a way forbidden by its access permissions` on 50050-50052 / 6060-6062 / 8080-8081 | Hyper-V/WSL reserved that port range. Check in PowerShell: `netsh interface ipv4 show excludedportrange protocol=tcp`. Fix (Admin): `net stop winnat; net start winnat`, then `make up` again |
| `/usr/bin/env: 'sh\r': No such file or directory` | scripts have CRLF endings: re-clone inside WSL, or `git config --global core.autocrlf input` and re-checkout |
| `permission denied` on `/var/run/docker.sock` | `sudo usermod -aG docker $USER`, then reopen the terminal |
| `Cannot connect to the Docker daemon` | Docker Desktop not running / WSL integration disabled for the distro; with Docker Engine in WSL: `sudo service docker start` |
| `profiles/*` owned by root, cannot delete | files are written by the container as root: `make clean-profiles` |
| Build killed while compiling tools | raise `memory=` in `.wslconfig`, `wsl --shutdown` |

### Without WSL / without `make` (PowerShell + Docker Desktop)

Possible but not recommended. Clone with LF line endings
(`git clone --config core.autocrlf=false <repo-url>`) and replace `make` targets with
`docker compose` commands:

| make | PowerShell |
|---|---|
| `make up` | `docker compose up -d --build --wait` |
| `make check` | `docker compose exec -T toolbox ./scripts/check.sh` |
| `make test-v2-flat` | `docker compose exec -T toolbox ./scripts/shoot.sh v2_flat` |
| `make profile-v2-fs` | `docker compose exec -T toolbox ./scripts/profile.sh v2_flat feature-store,orchestrator v1_heavy` |
| `make profile-v4-pooled` | `docker compose exec -T -e FOCUS="fetchFatCandidates\|ReturnToVTPool\|ResetVT\|FromVTPool\|PutBatch\|CustomResetBatch\|GetBatch" toolbox ./scripts/profile.sh v4_pooled orchestrator v3_vtpool` |
| `make bench-compare` | `docker compose exec -T toolbox sh -c "go test -run='^$' -bench=. -benchmem -count=6 ./internal/pooling/ \| tee profiles/bench.txt && benchstat profiles/bench.txt"` |
| `make pprof-ui SVC=orchestrator AMMO=v4_pooled KIND=cpu` | `docker compose exec toolbox ./scripts/pprof_ui.sh orchestrator v4_pooled cpu` |
| `make down` | `docker compose down` |

All other targets follow the same pattern; see the `Makefile`.

---

## Memory budget

Measured peak RSS (native run with `GOMAXPROCS=4` and `go -p 4`, i.e. like a 4-CPU VM,
cold build cache):

| Phase | Peak | Dominated by |
|---|---|---|
| Image build | ~1.05 GiB | `go install buf` (steps run one by one; `go build ./cmd/...` ~0.7 GiB) |
| Workshop load, ~240 rps, concurrency 16 | ~0.77 GiB | `v2_flat` / `v1_heavy`: orchestrator ~260 MiB + feature-store ~305 MiB + ghz ~150 MiB + candidates ~55 MiB |
| `make bench-compare` | ~0.23 GiB | runs without load |
| `go tool pprof` / graphviz | < 0.1 GiB | |

Add ~0.5 GiB for the VM kernel, dockerd/containerd and healthchecks: the build needs
about 1.6 GiB and the workshop about 1.3 GiB, so 4 GB leaves a ~2.5x margin.

Memory grows with load concurrency: in `v1_heavy` each in-flight request costs the
orchestrator ~15 MiB, so `CONCURRENCY=64` adds roughly 0.7 GiB. More VM CPUs also
raise the build peak (more packages compile in parallel).

## Before the workshop: checklist

```sh
make up && make check            # stand builds and answers
make test-v1-small               # ghz works, P99 is reported
make profile-v4-pooled           # profiles are captured into ./profiles
make bench-compare               # benchstat table is printed
make down
```

After that, the image is cached locally: `make up` at the workshop takes seconds
and needs no network.

## Cleanup after the workshop

```sh
make down
docker image rm recsys-workshop:latest
docker builder prune            # build cache
colima stop                     # macOS: stop the VM (colima delete removes it)
```

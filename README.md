# Devlink — .localhost Development Gateway

## 🇰🇷 한국어

### 소개
Devlink은 로컬 개발 환경에서 `*.localhost` 도메인을 대상으로 프로덕션과 유사한 HTTPS 환경을 손쉽게 구축할 수 있는 게이트웨이입니다. 자동으로 루트 인증서를 생성하여 신뢰할 수 있게 안내하고, HTTP→HTTPS 리디렉션과 `X-Forwarded-Proto=https` 헤더 주입을 수행하며, `Domain=localhost` 쿠키를 자동으로 보정합니다. 또한 HTTP 및 WebSocket 업스트림에 대한 프록시를 지원하고 필요 시 경로 접두어 제거도 처리합니다.

### 주요 기능
- 여러 프로젝트를 관리하고 프로젝트마다 하나 이상의 `*.localhost` 도메인을 연결할 수 있습니다.
- 기본 프런트엔드(`/`)와 백엔드(`/api`) 라우트를 자동으로 구성하며 SPA Fallback 옵션을 제공합니다.
- HTTP 및 WebSocket 업스트림을 대상으로 하는 추가 경로 접두어를 자유롭게 등록할 수 있습니다.
- `~/.devlink/`에 저장되는 자체 서명 CA를 기반으로 HTTPS 인증서를 자동으로 발급합니다.
- 브라우저 간 로그인/세션 유지를 위해 `Domain=localhost` 쿠키를 자동으로 올바른 도메인으로 변경합니다.
- `fsnotify`를 사용해 구성 파일 변경을 감지하고 실시간으로 라우트를 갱신합니다.

### 설치
#### 릴리스 바이너리 다운로드
아래 명령에서 `YOUR_GITHUB_OWNER` 부분을 실제 GitHub 소유자(개인 계정 또는 조직) 이름으로 바꿔 사용하세요. 최신 릴리스는 `https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest`에서 확인할 수 있습니다.

##### Linux (amd64)
```bash
curl -L "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-linux-amd64.tar.gz" -o devlink-linux-amd64.tar.gz
tar -xzf devlink-linux-amd64.tar.gz
sudo install -m 0755 devlink-linux-amd64/devlink /usr/local/bin/devlink
```

##### macOS (amd64/Intel)
```bash
curl -L "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-darwin-amd64.tar.gz" -o devlink-darwin-amd64.tar.gz
tar -xzf devlink-darwin-amd64.tar.gz
sudo install -m 0755 devlink-darwin-amd64/devlink /usr/local/bin/devlink
```

##### macOS (arm64/Apple Silicon)
```bash
curl -L "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-darwin-arm64.tar.gz" -o devlink-darwin-arm64.tar.gz
tar -xzf devlink-darwin-arm64.tar.gz
sudo install -m 0755 devlink-darwin-arm64/devlink /usr/local/bin/devlink
```

##### Windows (amd64)
PowerShell(관리자)에서 실행합니다. 원하는 설치 경로(예: `C:\Tools\devlink`)를 미리 만들어 두고 `Move-Item`에서 해당 경로를 지정하세요.
```powershell
Invoke-WebRequest -Uri "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-windows-amd64.zip" -OutFile devlink-windows-amd64.zip
Expand-Archive -Path devlink-windows-amd64.zip -DestinationPath .
Move-Item -Path .\devlink-windows-amd64\devlink.exe -Destination "C:\\Tools\\devlink\\devlink.exe" -Force
setx PATH "$Env:PATH;C:\\Tools\\devlink"
```

PATH에 반영된 뒤에는 새로운 터미널을 열고 `devlink --help`로 설치를 확인할 수 있습니다.

#### 수동 빌드
```bash
# 바이너리 빌드
make build  # 또는 go build ./cmd/devlink
```
빌드된 `devlink` 바이너리를 `$PATH`에 위치시키면 됩니다.

### 구성 파일
구성은 `${DEVLINK_CONFIG}` 또는 해당 변수가 비어 있는 경우 `$XDG_CONFIG_HOME/devlink/devlink.yaml`(기본값은 `~/.devlink/devlink.yaml`) 경로의 YAML 파일에 저장됩니다. 스키마는 다음과 같습니다.

```yaml
projects:
  first:
    domains: [first.localhost]
    routes:
      - path: "/"
        upstream: "http://127.0.0.1:5173"
        spaFallback: true
      - path: "/api"
        upstream: "http://127.0.0.1:8080"
        stripPathPrefix: true
```

### 사용법
#### 게이트웨이 실행
```bash
devlink serve
```
이 명령은 구성 파일을 생성(필요한 경우)하고, 루트/도메인 인증서를 준비한 뒤 :80에서 HTTP 리디렉션을, :443에서 HTTPS 프록시 트래픽을 처리합니다. 또한 구성 파일 변경을 감시하여 실시간으로 반영합니다.

#### 프로젝트 관리
프런트엔드와 백엔드를 지정하여 프로젝트를 추가하거나 갱신합니다.
```bash
devlink add first \
  --domain first.localhost \
  --front  http://127.0.0.1:5173 \
  --backend http://127.0.0.1:8080
```

경로 옵션을 함께 지정해 라우트를 추가할 수 있습니다.
```bash
devlink add first \
  --domain first.localhost \
  --route /api=http://127.0.0.1:8080;strip \
  --route /gql=http://127.0.0.1:8081;strip \
  --route /gql/subscriptions=ws://127.0.0.1:8082;keep;websocket
```

옵션 설명:
- `strip`(기본값) – 업스트림으로 전달할 때 접두어를 제거합니다.
- `keep` – 접두어를 제거하지 않고 그대로 전달합니다.
- `websocket` – 업스트림이 WebSocket 트래픽을 주로 처리함을 알립니다.
- `spa` – SPA Fallback 처리를 활성화합니다.

프로젝트 조회 및 삭제:
```bash
devlink list
devlink remove first
```

### HTTPS 신뢰 설정
Devlink은 생성한 루트 CA를 `~/.devlink/devlink-ca.pem`에 저장합니다. 처음 실행할 때 운영체제/브라우저 신뢰 저장소에 이 인증서를 설치해야 합니다. 게이트웨이는 만료가 임박한 `*.localhost` 인증서를 자동으로 갱신합니다.

---

## 🇺🇸 English

### Overview
Devlink is a zero-config HTTPS gateway for local development that delivers production-like origin isolation for `*.localhost` domains. It automatically creates and trusts a local certificate authority, performs HTTP→HTTPS redirection, injects `X-Forwarded-Proto=https`, rewrites legacy `Domain=localhost` cookies, and supports HTTP/WebSocket proxies with optional prefix stripping.

### Features
- Manage multiple projects, each with one or more `*.localhost` domains.
- Automatic routes for frontends (`/`) and backends (`/api` by default) with optional SPA fallback handling.
- Add additional path prefixes that can point at HTTP or WebSocket upstreams.
- Automatic HTTPS via a self-signed CA stored under `~/.devlink/`.
- Cookie domain rewriting to keep login/session flows working across browsers.
- Live reload of configuration through `fsnotify`.

### Installation
#### Download prebuilt release assets
Replace `YOUR_GITHUB_OWNER` with the actual GitHub owner (user or organization) for this repository. The latest release is available at `https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest`.

##### Linux (amd64)
```bash
curl -L "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-linux-amd64.tar.gz" -o devlink-linux-amd64.tar.gz
tar -xzf devlink-linux-amd64.tar.gz
sudo install -m 0755 devlink-linux-amd64/devlink /usr/local/bin/devlink
```

##### macOS (amd64/Intel)
```bash
curl -L "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-darwin-amd64.tar.gz" -o devlink-darwin-amd64.tar.gz
tar -xzf devlink-darwin-amd64.tar.gz
sudo install -m 0755 devlink-darwin-amd64/devlink /usr/local/bin/devlink
```

##### macOS (arm64/Apple Silicon)
```bash
curl -L "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-darwin-arm64.tar.gz" -o devlink-darwin-arm64.tar.gz
tar -xzf devlink-darwin-arm64.tar.gz
sudo install -m 0755 devlink-darwin-arm64/devlink /usr/local/bin/devlink
```

##### Windows (amd64)
Run the following in an elevated PowerShell session. Create your preferred install directory first (e.g., `C:\Tools\devlink`) and update `Move-Item` accordingly.
```powershell
Invoke-WebRequest -Uri "https://github.com/YOUR_GITHUB_OWNER/local-ssl/releases/latest/download/devlink-windows-amd64.zip" -OutFile devlink-windows-amd64.zip
Expand-Archive -Path devlink-windows-amd64.zip -DestinationPath .
Move-Item -Path .\devlink-windows-amd64\devlink.exe -Destination "C:\\Tools\\devlink\\devlink.exe" -Force
setx PATH "$Env:PATH;C:\\Tools\\devlink"
```

After the PATH update, open a new terminal and run `devlink --help` to verify the installation.

#### Build from source
```bash
# Build the binary
make build  # or go build ./cmd/devlink
```
Place the resulting `devlink` binary somewhere in your `$PATH`.

### Configuration
Configuration is stored in YAML at `${DEVLINK_CONFIG}` or, if unset, `$XDG_CONFIG_HOME/devlink/devlink.yaml` (falling back to `~/.devlink/devlink.yaml`). The schema matches the following shape:

```yaml
projects:
  first:
    domains: [first.localhost]
    routes:
      - path: "/"
        upstream: "http://127.0.0.1:5173"
        spaFallback: true
      - path: "/api"
        upstream: "http://127.0.0.1:8080"
        stripPathPrefix: true
```

### Usage
#### Start the gateway
```bash
devlink serve
```
This command ensures a configuration file exists, generates a CA/certificate if necessary, listens on :80 for HTTP redirects and :443 for HTTPS proxy traffic, and watches the configuration file for live changes.

#### Manage projects
Add or update a project, specifying a frontend and backend:
```bash
devlink add first \
  --domain first.localhost \
  --front  http://127.0.0.1:5173 \
  --backend http://127.0.0.1:8080
```

Add additional routes with inline options separated by `;`:
```bash
devlink add first \
  --domain first.localhost \
  --route /api=http://127.0.0.1:8080;strip \
  --route /gql=http://127.0.0.1:8081;strip \
  --route /gql/subscriptions=ws://127.0.0.1:8082;keep;websocket
```

Options:
- `strip` (default) – remove the prefix when proxying
- `keep` – retain the prefix for the upstream
- `websocket` – hint that the upstream primarily serves WebSocket traffic
- `spa` – enable SPA fallback handling

List or remove projects:
```bash
devlink list
devlink remove first
```

### HTTPS Trust
Devlink stores the generated root CA in `~/.devlink/devlink-ca.pem`. Install this certificate into your operating system/browser trust store the first time you run the proxy. The gateway automatically rotates the issued `*.localhost` certificate when it approaches expiry.

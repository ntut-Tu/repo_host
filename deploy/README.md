# Go HTTP 服務部署

所有路徑以 `tools` 根目錄（或部署包解開後的 `tools-server`）為準。
Go 服務使用 Docker container；Python usage 案例保留在外部，不隨服務部署。

## 直接部署或透過壓縮包搬移

可將整個 `tools` 資料夾放到伺服器；也可在 `tools` 根目錄執行：

```bash
./scripts/package_tools_server.sh
```

將產生的 `dist/tools-server.tar.gz` 複製到伺服器後：

```bash
tar -xzf tools-server.tar.gz
cd tools-server
```

伺服器需要 Docker Engine 與 Compose plugin。第一次建置需要能取得 Go／Alpine
base image 與 Go module 依賴。部署及打包均使用此目錄內的檔案。

## 設定與啟動

```bash
cp values.example.yaml values.yaml
chmod 600 values.yaml
```

編輯根目錄 `values.yaml` 的 `codehost_go`：

- `provider`、`repository`：目標 GitHub／GitLab repository。
- `upstream_token`：讀取上游平台的憑證；GitHub GraphQL 與私有 repository 需要設定。
- `api_token`：獨立產生至少 32 字元的服務 Bearer token，供使用端呼叫。
- `api_url`、`git_url`：自架平台時調整；Git URL 使用不帶帳密的 HTTPS 位址。
- `data_directory`：容器使用 `/data`，由持久化 volume 保存快取與 Git 物件。

用持有 `values.yaml` 的非 root 帳號執行：

```bash
./scripts/deploy.sh
curl --fail http://127.0.0.1:8082/health/live
```

腳本以目前帳號的 UID/GID 建置容器使用者，以讀取權限 600 的設定檔。
後續更新使用相同帳號。預設主機綁定位址為 `127.0.0.1:8082`；變更 host port 可執行：

```bash
CODEHOST_PORT=18082 ./scripts/deploy.sh
```

同主機反向代理可指向此位址。需要指定其他主機介面時，設定 `CODEHOST_BIND_IP`，
並在後續部署與 Compose 操作沿用相同值。對外服務的 TLS 與代理位址由部署環境設定。
`/health/live`、`/openapi.json`、`/docs` 公開，其餘資源 API 需要 Bearer token。

## 維運與更新

在根目錄執行：

```bash
docker compose -f deploy/compose.yaml ps
docker compose -f deploy/compose.yaml logs --tail=100 codehost-go
docker compose -f deploy/compose.yaml stop
docker compose -f deploy/compose.yaml start
```

更新時解開新版部署包，將原本的私有 `values.yaml` 複製到新版根目錄，再執行
`./scripts/deploy.sh`。Compose project name 固定為 `tools-codehost`，沿用相同的
`codehost-data` volume。主機故障的恢復需要另行備份 volume；`down --volumes`
會刪除持久資料。

## 打包邊界

`scripts/package_tools_server.sh` 只打包明確列出的 Go 原始碼、部署與說明檔案。
根目錄 `.dockerignore` 只允許 `codehost-go` 進入 Docker build context；runtime
image 只複製編譯後的執行檔。`usage-example` 與私有設定不進入壓縮包或映像。

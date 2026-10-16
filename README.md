# Code-host tools

Go 服務的部署、打包與設定都在此 `tools` 根目錄內，可整個資料夾帶到其他機器使用。

```text
codehost/                    Python 函式庫
codehost-go/                 Go 服務與 Dockerfile
usage-example/               獨立 Python 使用案例（不進入部署包或映像）
deploy/compose.yaml          Go 服務的 Docker Compose 設定
scripts/deploy.sh            部署入口
scripts/package_tools_server.sh  產生可攜式部署包
values.example.yaml          Go 服務設定範例
values.yaml                  自行填寫的私有設定
structure/README.md          實際架構與打包邊界
```

## 在伺服器部署

在本資料夾根目錄執行：

```bash
cp values.example.yaml values.yaml
chmod 600 values.yaml
# 編輯 values.yaml，填入 repository、服務 token 與上游 token
./scripts/deploy.sh
```

需要 Docker Engine 與 Compose plugin。使用持有 `values.yaml` 的非 root 帳號部署。
完整操作、連線方式與更新步驟見 [部署說明](deploy/README.md)。

## 打包帶走

```bash
./scripts/package_tools_server.sh
```

輸出在本資料夾的 `dist/tools-server.tar.gz`，包含 Go 原始碼、部署檔與設定範例。
`usage-example`、Python 函式庫與私有 `values.yaml` 不會被打包。
壓縮包解開後的部署指令與上述相同。單獨部署包不包含 usage 範例。

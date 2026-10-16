# Python 使用案例

兩個 `main.py` 都在檔案頂端直接放置 URL、token、編號、SHA、路徑等固定假值，
並在 `main()` 直接示範呼叫；沒有 argparse 或 YAML 設定流程。
示意值不代表可用的 repository 或服務，實際執行時會依這些值發出請求。

在 `tools/usage-example` 中執行（Python 3.11 以上與 uv）：

```bash
uv run --no-project --with ../codehost python codehost/main.py
uv run --no-project --with httpx python codehost-go/main.py
```

## Python 函式庫：codehost/main.py

同一個檔案分別示範 `GitHubApi` 與 `GitLabApi` 的全部公開方法。
兩者各有 8 個 `CodeHost` 共同方法與 5 個額外公開輔助方法。

| 方法 | 示範內容 |
| --- | --- |
| `fetch` | 正規化 PR/MR、異動檔案與來源 repository |
| `tree` | 固定 revision 的完整遞迴目錄 |
| `read_source` | 用 revision、path 讀取檔案 |
| `read_entry` | 用 tree 回傳的 TreeEntry 讀取檔案 |
| `get_commit` | 完整 SHA 的 commit metadata |
| `get_pr` | 資源格式的 PR/MR 與關聯 issue |
| `get_issue` | issue 與關聯 PR/MR |
| `get_pr_timeline` | commit、comment、merge 時間序列 |
| `root` | 產生 repository API 根 URL |
| `get` | repository-relative endpoint 的原始 JSON |
| `pages` | 迭代分頁 API 的項目 |
| `pull_request` | 原始 PR/MR JSON 與異動檔案列表 |
| GitHub `read_blob` | 用 blob SHA 讀取內容 |
| GitLab `read_file` | 用 path、revision、project 讀取內容 |

`fetch` 和 `get_pr` 是不同回傳格式，因此分別示範。檔案讀取使用 PR/MR 的
head SHA 與來源 repository，包含 fork。路徑需在對應版本存在。
GitHub 關聯資料使用 GraphQL，需要相應的上游 token。
`HttpClient` 在此作為 provider 的傳輸依賴注入。

## Go HTTP 服務：codehost-go/main.py

示範已部署服務目前的全部 12 個 endpoint：

| HTTP API | 示範內容 |
| --- | --- |
| `GET /health/live` | 存活檢查 |
| `GET /openapi.json` | OpenAPI JSON |
| `GET /docs` | API 文件 HTML |
| `GET /v1/commits/{sha}` | commit metadata |
| `GET /v1/prs/{number}` | PR/MR metadata |
| `GET /v1/issues/{number}` | issue metadata |
| `GET /v1/prs/{number}/timeline` | 時間序列 |
| `POST /v1/revisions/resolve` | branch、tag、sha、pr 四種輸入 |
| `GET /v1/trees/{sha}` | path、recursive、limit、cursor 分頁 |
| `GET /v1/files/{sha}` | 完整檔案及 start/end 行範圍 |
| `POST /v1/files/batch` | 批次讀檔，可對每個檔案指定行範圍 |
| `POST /v1/search` | 純文字與 regex 搜尋 |

前三個 endpoint 是公開讀取，其餘會加入 Bearer token。來源讀取固定使用
branch resolve 回傳的 SHA；目錄分頁沿用原查詢條件。批次讀取需注意單筆
`error`；搜尋的 `complete` 與 `omitted` 表示結果是否完整。

這些示範循序執行，遇到錯誤會停止；可單獨取用需要的程式段落。
`usage-example` 不會隨 Go 服務打包、建置或部署到 container。

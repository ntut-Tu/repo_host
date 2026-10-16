# Codehost Lite 開發者 Runbook

## 用途與檔案位置

這是獨立 Python 函式庫，支援 GitHub、GitLab.com 與自架 GitLab。Python 3.11 以上，使用 uv。

```text
tools/
├── codehost-lite/
│   ├── pyproject.toml
│   ├── RUNBOOK.md
│   ├── src/codehost_lite/
│   │   ├── models.py       # PR、issue、commit 的 dataclass
│   │   ├── http.py         # HTTP 請求、輸入檢查、錯誤處理
│   │   ├── github.py       # GitHub 欄位轉換
│   │   ├── gitlab.py       # GitLab 欄位轉換
│   │   └── __init__.py     # 公開匯入
│   └── tests/
└── usage-example/codehost-lite/main.py
```

只讀取基本資源資料；沒有 timeline、cache、Git clone、資料庫、HTTP server 或 container。每個方法一次 GET，不預抓關聯資源、不自動重試。GitHub commit endpoint 本身可能附帶 files，函式庫只回傳 commit metadata，不追加 files 分頁。

## 1. 直接跑 demo

```bash
cd /home/mo/Documents/tools/usage-example
uv run --no-project --with ../codehost-lite python codehost-lite/main.py
```

所有輸入都在 `usage-example/codehost-lite/main.py` 寫死；沒有 argparse、環境變數設定或 values.yaml。預設讀取 `ntut-Tu/cloth_shop_server` 的 PR #31、issue #29 和固定 commit SHA，空白 GitHub token 可讀公開 repo，仍受 GitHub 的未登入額度限制。

GitLab 範例預設 `RUN_GITLAB = False`，避免呼叫假站台。填好下列固定值，再改成 `True`，同一次執行就會示範兩個平台的全部三個方法：

```python
GITLAB_API_URL = "https://gitlab.your-company.example/api/v4"
GITLAB_REPOSITORY = "team/subgroup/project"
GITLAB_TOKEN = ""  # 在本機填入有該專案讀取權限的 access token。
GITLAB_MR_IID = 1
GITLAB_ISSUE_IID = 2
GITLAB_COMMIT_REF = "main"
```

`GITLAB_API_URL` 是完整 API v4 根路徑。若安裝在子路徑，填 `https://host/gitlab/api/v4`。`GITLAB_REPOSITORY` 填未編碼的專案路徑，由函式庫處理子群組與 URL encoding。MR／issue 使用頁面上的專案內編號 IID，不是全站 ID。自架 GitLab 使用內部 CA 時，填入 `GITLAB_CA_FILE` 的 PEM 檔絕對路徑；範例透過 SSL context 驗證憑證。

GitHub、GitLab 各用自己的 HTTP client。真實 token 僅在本機填入，不要提交含 token 的檔案。

## 2. 搬到另一台電腦或專案

只需複製完整的 `codehost-lite/` 資料夾。若要一起跑範例，保持以下相對位置：

```text
/your/demo/
├── codehost-lite/                     # 從 tools/codehost-lite 複製
└── usage-example/codehost-lite/       # 從 tools/usage-example/codehost-lite 複製
```

```bash
cd /your/demo/usage-example
uv run --no-project --with ../codehost-lite python codehost-lite/main.py
```

加入既有 uv Python 專案時，可將套件放進該專案的 `vendor/codehost-lite/`：

```bash
cd /your/application
uv add ./vendor/codehost-lite
```

應用程式直接匯入並組合 client 與 provider：

```python
import httpx
from codehost_lite import GitHub, GitLab

with httpx.Client(timeout=30.0) as client:
    github = GitHub("ntut-Tu/cloth_shop_server", client, token="")
    pr = github.get_pr(31)
    issue = github.get_issue(29)
    commit = github.get_commit(pr.head_sha)
    print(pr.title, issue.title, commit.sha)

with httpx.Client(timeout=30.0) as client:
    gitlab = GitLab(
        "team/subgroup/project", client, token="LOCAL_ACCESS_TOKEN",
        api_url="https://gitlab.your-company.example/api/v4",
    )
    mr = gitlab.get_pr(1)
    issue = gitlab.get_issue(2)
    commit = gitlab.get_commit("main")
```

呼叫者負責 HTTP client 的生命週期、timeout、proxy 和 TLS 設定。需要讀另一個 repo 時，建立另一個 provider instance；套件內沒有寫死 demo repo。原有 `codehost`、`codehost-go` 與 Agent 都不是此套件的依賴。

## 3. 方法與回傳值

| 方法 | 回傳 | 內容 |
| --- | --- | --- |
| `get_pr(number)` | `PullRequest` | number、title、description、state、url、head_sha、source_branch、target_branch |
| `get_issue(number)` | `Issue` | number、title、description、state、url |
| `get_commit(ref)` | `Commit` | sha、message、committed_at、author_name、url |

三種回傳都是不可變 dataclass，可以用 `dataclasses.asdict()` 轉 dict。時間保留上游 ISO 8601 字串；沒有資料時為 `None`。空白 description 轉成空字串。GitLab `opened` 統一為 `open`；已合併 PR/MR 為 `merged`。GitLab 新建立 MR 尚未備妥時，`head_sha` 可能為 `None`。

Commit ref 可用完整 SHA、branch 或 tag；branch/tag 會隨上游變更，以回傳的 `sha` 識別這次結果。需要固定版本時直接傳 SHA。

資源不包含 comments、diff、changed files、關聯 issue/PR 或歷史列表。`get_issue()` 收到 GitHub PR 編號會明確報錯，請改用 `get_pr()`。

## 4. 執行測試

```bash
cd /your/demo/codehost-lite
uv run --no-project --with . python -m unittest discover -s tests -p 'test_*.py' -v
uv run --no-project --with . python tests/live_github.py
```

第一行測試不連線。GitHub fixtures 是指定 demo repo 的公開基本資料；GitLab 使用模擬自架網址，涵蓋子路徑、子群組、token header、IID、欄位轉換及每次重新請求。另測試 HTTP 錯誤、timeout、無效輸入、損壞回應與禁止轉址。

第二行明確連線 GitHub，只讀 PR #31、issue #29 和固定 commit，不寫入 repo。GitLab 尚未連線驗證你的實際自架站台；填入範例常數後可執行三個資源讀取確認。

## 5. 排錯

`ValueError` 表示本機輸入不合法。`CodeHostError.status_code` 可用於判斷 HTTP 問題；網路、timeout 或回應格式錯誤時為 `None`。錯誤文字不附 token、URL 或上游回應內文。

- 401／403：檢查 token、repo 存取權限及 API 額度。
- 404：檢查 repo 路徑、PR／issue 編號或 commit ref；私有 repo 權限不足也可能顯示 404。
- 429：已達請求額度，稍後再試。
- 3xx：不會自動轉址；請填入站台最終 API URL。
- 連線錯誤：檢查 VPN、DNS、proxy 及自架站台的 CA 設定。

API 欄位依據：[GitHub PR](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request)、[GitHub commit](https://docs.github.com/en/rest/commits/commits#get-a-commit)、[GitLab MR](https://docs.gitlab.com/api/merge_requests/#get-single-mr)、[GitLab issue](https://docs.gitlab.com/api/issues/#single-project-issue)、[GitLab commit](https://docs.gitlab.com/api/commits/#get-a-single-commit)。

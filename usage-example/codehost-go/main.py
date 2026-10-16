"""用 Python 直接示範 Go 服務的全部 12 個 HTTP endpoint；以下皆為假資料。"""

from pprint import pprint

import httpx

CODEHOST_URL = "http://localhost/"  # 已部署 Go 服務的位址
CODEHOST_TOKEN = "example-service-token-not-a-real-secret"
PR_NUMBER = 31  # GitLab 對應 MR IID
ISSUE_NUMBER = 10
COMMIT_SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"  # 必須是完整 SHA
BRANCH = "main"
TAG = "v1.0.0"
FILE_PATH = "README.md"
SECOND_FILE_PATH = "src/main.py"
TREE_PATH = ""  # 空字串表示 repository 根目錄
TREE_LIMIT = 100
LINE_START = 1  # 行號從 1 開始，end 包含該行
LINE_END = 20
SEARCH_PATH = "src"
SEARCH_TEXT = "TODO"
SEARCH_REGEX = r"TODO|FIXME"
SEARCH_LIMIT = 50


def main():
    with httpx.Client(base_url=CODEHOST_URL, timeout=70, follow_redirects=False) as client:
        # 1. GET /health/live：公開的存活檢查。
        response = client.get("health/live")
        response.raise_for_status()
        pprint(response.json())

        # 2. GET /openapi.json：公開的 OpenAPI 規格。
        response = client.get("openapi.json")
        response.raise_for_status()
        pprint(response.json())

        # 3. GET /docs：公開的 API 頁面，回傳 HTML。
        response = client.get("docs")
        response.raise_for_status()
        print(response.text)

        # 以下資源 API 都需要 Bearer token。
        client.headers["Authorization"] = f"Bearer {CODEHOST_TOKEN}"

        # 4. GET /v1/commits/{sha}：commit metadata。
        response = client.get(f"v1/commits/{COMMIT_SHA}")
        response.raise_for_status()
        pprint(response.json())

        # 5. GET /v1/prs/{number}：PR/MR metadata。
        response = client.get(f"v1/prs/{PR_NUMBER}")
        response.raise_for_status()
        pprint(response.json())

        # 6. GET /v1/issues/{number}：issue metadata。
        response = client.get(f"v1/issues/{ISSUE_NUMBER}")
        response.raise_for_status()
        pprint(response.json())

        # 7. GET /v1/prs/{number}/timeline：commit、comment、merge 時間序列。
        response = client.get(f"v1/prs/{PR_NUMBER}/timeline")
        response.raise_for_status()
        pprint(response.json())

        # 8. POST /v1/revisions/resolve：四種 kind 都可轉成固定 SHA。
        response = client.post("v1/revisions/resolve", json={"kind": "branch", "value": BRANCH})
        response.raise_for_status()
        revision = response.json()
        pprint(revision)
        source_sha = revision["sha"]  # 後續讀取固定在此次解析出的 branch 版本。

        response = client.post("v1/revisions/resolve", json={"kind": "tag", "value": TAG})
        response.raise_for_status()
        pprint(response.json())

        response = client.post("v1/revisions/resolve", json={"kind": "sha", "value": COMMIT_SHA})
        response.raise_for_status()
        pprint(response.json())

        response = client.post("v1/revisions/resolve", json={"kind": "pr", "value": str(PR_NUMBER)})
        response.raise_for_status()
        pprint(response.json())

        # 9. GET /v1/trees/{sha}：指定目錄、遞迴及分頁大小。
        tree_params = {"path": TREE_PATH, "recursive": "true", "limit": TREE_LIMIT}
        response = client.get(f"v1/trees/{source_sha}", params=tree_params)
        response.raise_for_status()
        page = response.json()
        pprint(page)
        # next 是服務回傳的游標；換頁時保留其餘條件與 SHA。
        while page.get("next"):
            response = client.get(
                f"v1/trees/{source_sha}", params={**tree_params, "cursor": page["next"]},
            )
            response.raise_for_status()
            page = response.json()
            pprint(page)

        # 10. GET /v1/files/{sha}：讀取完整檔案。
        response = client.get(f"v1/files/{source_sha}", params={"path": FILE_PATH})
        response.raise_for_status()
        pprint(response.json())

        # 同一個 API 可指定行範圍。
        response = client.get(
            f"v1/files/{source_sha}",
            params={"path": FILE_PATH, "start": LINE_START, "end": LINE_END},
        )
        response.raise_for_status()
        pprint(response.json())

        # 11. POST /v1/files/batch：一個 SHA 下批次讀取多個檔案。
        response = client.post("v1/files/batch", json={
            "sha": source_sha,
            "files": [
                {"path": FILE_PATH},
                {"path": SECOND_FILE_PATH, "start": LINE_START, "end": LINE_END},
            ],
        })
        response.raise_for_status()
        batch = response.json()
        pprint(batch)  # HTTP 200 仍可能有單一 item 的 error，要逐筆檢查。

        # 12. POST /v1/search：固定 SHA 的文字搜尋。
        response = client.post("v1/search", json={
            "sha": source_sha, "path": SEARCH_PATH,
            "query": SEARCH_TEXT, "regex": False, "limit": SEARCH_LIMIT,
        })
        response.raise_for_status()
        pprint(response.json())  # complete / omitted 說明結果是否受 budget 限制。

        # 同一個 API 的 regex 搜尋（服務使用 Go regexp 語法）。
        response = client.post("v1/search", json={
            "sha": source_sha, "path": SEARCH_PATH,
            "query": SEARCH_REGEX, "regex": True, "limit": SEARCH_LIMIT,
        })
        response.raise_for_status()
        pprint(response.json())


if __name__ == "__main__":
    main()

"""直接示範 GitHubApi、GitLabApi 的所有公開方法；以下皆為假資料。"""

from pprint import pprint

from codehost import GitHubApi, GitLabApi
from codehost.http import HttpClient

GITHUB_URL = "https://api.github.com"
GITHUB_REPOSITORY = "example-owner/example-repo"
GITHUB_TOKEN = "example-github-token"
GITHUB_PR_NUMBER = 31
GITHUB_ISSUE_NUMBER = 10
GITHUB_COMMIT_SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
GITHUB_FILE_PATH = "README.md"

GITLAB_URL = "https://gitlab.example.com/api/v4"
GITLAB_PROJECT = "example-group/example-project"  # 也可填數字 project ID 字串
GITLAB_TOKEN = "example-gitlab-token"
GITLAB_MR_NUMBER = 31  # repository 內的 MR IID
GITLAB_ISSUE_NUMBER = 10
GITLAB_COMMIT_SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
GITLAB_FILE_PATH = "README.md"


def main():
    github = GitHubApi(
        repository=GITHUB_REPOSITORY,
        token=GITHUB_TOKEN,
        client=HttpClient(timeout=30),
        api_url=GITHUB_URL,
    )

    print("GitHub：CodeHost 的 8 個共同方法")

    # 1. fetch：正規化 PR 資料，包含異動檔案與來源 repository。
    pr = github.fetch(GITHUB_PR_NUMBER)
    pprint(pr)

    # 2. tree：列出固定 revision 的完整遞迴目錄，支援 fork repository。
    entries = github.tree(pr.head_ref, pr.head_repository)
    pprint(entries)

    # 3. read_source：用 revision 與路徑讀取檔案。
    print(github.read_source(pr.head_ref, GITHUB_FILE_PATH, pr.head_repository))

    # 4. read_entry：使用 tree 回傳的 TreeEntry 讀取檔案。
    entry = next(item for item in entries if item.path == GITHUB_FILE_PATH and item.type == "blob")
    print(github.read_entry(entry, pr.head_ref, pr.head_repository))

    # 5. get_commit：讀取完整 SHA 對應的 commit。
    pprint(github.get_commit(GITHUB_COMMIT_SHA))

    # 6. get_pr：讀取資源格式的 PR（含狀態、head SHA、關聯 issue）。
    pprint(github.get_pr(GITHUB_PR_NUMBER))

    # 7. get_issue：讀取 issue 與關聯 PR。
    pprint(github.get_issue(GITHUB_ISSUE_NUMBER))

    # 8. get_pr_timeline：讀取 commit、comment、merge 的時間序列。
    pprint(github.get_pr_timeline(GITHUB_PR_NUMBER))

    print("GitHub：額外的 5 個公開輔助方法")

    # root：產生 repository API 根 URL；本身不發出請求。
    print(github.root())

    # get：讀取 repository-relative endpoint 的原始 JSON。
    pprint(github.get(f"/pulls/{GITHUB_PR_NUMBER}"))

    # pages：走訪分頁 API；每次迭代取得一筆項目。
    for item in github.pages(f"/pulls/{GITHUB_PR_NUMBER}/files"):
        pprint(item)

    # pull_request：原始 PR JSON 與完整異動檔案清單。
    metadata, changed_files = github.pull_request(GITHUB_PR_NUMBER)
    pprint(metadata)
    pprint(changed_files)

    # read_blob：以 blob SHA 讀取內容；參數是含 path/sha 的 dict。
    print(github.read_blob({"path": entry.path, "sha": entry.blob_id}, pr.head_repository))

    gitlab = GitLabApi(
        project=GITLAB_PROJECT,
        token=GITLAB_TOKEN,
        client=HttpClient(timeout=30),
        api_url=GITLAB_URL,
    )

    print("GitLab：CodeHost 的 8 個共同方法")

    # fetch：GitLab 使用 MR IID，回傳同一種正規化 PullRequest。
    mr = gitlab.fetch(GITLAB_MR_NUMBER)
    pprint(mr)

    # tree：第二個位置參數指定來源 project（支援 fork）。
    entries = gitlab.tree(mr.head_ref, mr.head_repository)
    pprint(entries)

    # read_source：固定 revision 的檔案文字。
    print(gitlab.read_source(mr.head_ref, GITLAB_FILE_PATH, mr.head_repository))

    # read_entry：使用 tree 回傳的 TreeEntry。
    entry = next(item for item in entries if item.path == GITLAB_FILE_PATH and item.type == "blob")
    print(gitlab.read_entry(entry, mr.head_ref, mr.head_repository))

    pprint(gitlab.get_commit(GITLAB_COMMIT_SHA))
    pprint(gitlab.get_pr(GITLAB_MR_NUMBER))
    pprint(gitlab.get_issue(GITLAB_ISSUE_NUMBER))
    pprint(gitlab.get_pr_timeline(GITLAB_MR_NUMBER))

    print("GitLab：額外的 5 個公開輔助方法")
    print(gitlab.root())
    pprint(gitlab.get(f"/merge_requests/{GITLAB_MR_NUMBER}"))
    for item in gitlab.pages(f"/merge_requests/{GITLAB_MR_NUMBER}/diffs"):
        pprint(item)

    metadata, changed_files = gitlab.pull_request(GITLAB_MR_NUMBER)
    pprint(metadata)
    pprint(changed_files)

    # read_file：注意參數順序是 path、revision、project。
    print(gitlab.read_file(GITLAB_FILE_PATH, mr.head_ref, mr.head_repository))


if __name__ == "__main__":
    main()

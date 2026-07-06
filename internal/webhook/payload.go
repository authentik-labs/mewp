package webhook

type PullRequestEvent struct {
	Action       string       `json:"action"`
	Label        *Label       `json:"label"`
	PullRequest  PullRequest  `json:"pull_request"`
	Repository   Repository   `json:"repository"`
	Installation Installation `json:"installation"`
}

type IssueEvent struct {
	Action       string       `json:"action"`
	Label        *Label       `json:"label"`
	Issue        Issue        `json:"issue"`
	Repository   Repository   `json:"repository"`
	Installation Installation `json:"installation"`
}

type PullRequest struct {
	Number         int     `json:"number"`
	Title          string  `json:"title"`
	Merged         bool    `json:"merged"`
	MergeCommitSHA string  `json:"merge_commit_sha"`
	Labels         []Label `json:"labels"`
	User           User    `json:"user"`
	State          string  `json:"state"`
}

type Issue struct {
	Number      int                      `json:"number"`
	PullRequest *IssuePullRequestPointer `json:"pull_request"`
}

type IssuePullRequestPointer struct {
	URL string `json:"url"`
}

type Label struct {
	Name string `json:"name"`
}

type Repository struct {
	FullName string    `json:"full_name"`
	Owner    RepoOwner `json:"owner"`
	Name     string    `json:"name"`
}

type RepoOwner struct {
	Login string `json:"login"`
}

type User struct {
	Login string `json:"login"`
}

type Installation struct {
	ID int64 `json:"id"`
}

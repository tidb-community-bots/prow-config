package formatchecker

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ti-community-infra/tichi/internal/pkg/externalplugins"
	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/github/fakegithub"

	_ "embed"
)

const (
	issueTitleRegex         = "^(\\[TI-(?P<issue_number>[1-9]\\d*)\\])+.+: .{10,160}$"
	issueNumberRegex        = "#([1-9]\\d*)"
	testTaskCheckedRegex    = `<!-- At least one of them must be included\. -->\s*[\n]{1,}(\- \[[xX ]\] .*\n)*(\- \[[xX]\] .*\n)(\- \[[xX ]\] .*\n)*\n` //nolint: lll
	issueNumberPrefixRegex  = "((https|http)://github\\.com/{{.Org}}/{{.Repo}}/issues/|{{.Org}}/{{.Repo}}#|#)"
	keywordPrefixRegex      = "(ref|close[sd]?|resolve[sd]?|fix(e[sd])?)"
	issueNumberLineTemplate = "(?im)^Issue Number:\\s*((,\\s*)?%s\\s*%s(?P<issue_number>[1-9]\\d*))+"
)

var (
	issueNumberLineRegexp = fmt.Sprintf(issueNumberLineTemplate, keywordPrefixRegex, issueNumberPrefixRegex)
)

//go:embed test-task-body.md
var testTaskBody string

func TestHandlePullRequestEvent(t *testing.T) {
	formattedLabel := func(label string) string {
		return fmt.Sprintf("%s/%s#%d:%s", "org", "repo", 1, label)
	}

	earlierCreatedAt, err := time.Parse(time.RFC3339, "2021-10-01T12:00:00Z")
	if err != nil {
		t.Error(err)
		return
	}

	startTime, err := time.Parse(time.RFC3339, "2021-11-01T12:00:00Z")
	if err != nil {
		t.Error(err)
		return
	}

	laterCreatedAt, err := time.Parse(time.RFC3339, "2021-12-01T12:00:00Z")
	if err != nil {
		t.Error(err)
		return
	}

	testcases := []struct {
		name   string
		action github.PullRequestEventAction
		// label that will be labeled or unlabeled.
		label     string
		title     string
		body      string
		branch    string
		createdAt time.Time
		// labels is the labels existed on the pull request (after the labeled / unlabeled event happened).
		labels             []string
		commitMessages     []string
		requiredMatchRules []externalplugins.RequiredMatchRule

		expectAddedLabels   []string
		expectDeletedLabels []string
		shouldComment       bool
	}{
		{
			name:      "PR title with issue number",
			action:    github.PullRequestActionOpened,
			title:     "[TI-12345] pkg: what's changed (#999)",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:    true,
					Title:          true,
					Regexp:         issueTitleRegex,
					MissingLabel:   "do-not-merge/invalid-title",
					MissingMessage: "Please fill in the title of the PR according to the prescribed format.",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
			shouldComment:       false,
		},
		{
			name:      "PR title without issue number",
			action:    github.PullRequestActionOpened,
			title:     "invalid title",
			labels:    []string{},
			branch:    "main",
			createdAt: earlierCreatedAt,
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:    true,
					Title:          true,
					Regexp:         issueTitleRegex,
					MissingLabel:   "do-not-merge/invalid-title",
					MissingMessage: "Please fill in the title of the PR according to the prescribed format.",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
			shouldComment:       true,
		},
		{
			name:      "PR body without issue number",
			action:    github.PullRequestActionOpened,
			body:      `PR Body content`,
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Body:         true,
					Regexp:       issueNumberRegex,
					MissingLabel: "do-not-merge/needs-issue-number",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/needs-issue-number"),
			},
			expectDeletedLabels: []string{},
			shouldComment:       false,
		},
		{
			name:   "PR body with issue number",
			action: github.PullRequestActionOpened,
			body: `
			PR Body content
			close #12345
`,
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Body:         true,
					Regexp:       issueNumberRegex,
					MissingLabel: "do-not-merge/needs-issue-number",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR body with cross-repository issue number",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			body:      "\r\n\r\nIssue Number: close org2/repo2#12345\r\n\r\n",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Body:         true,
					Regexp:       issueNumberLineRegexp,
					MissingLabel: "do-not-merge/needs-linked-issue",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/needs-linked-issue"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR body with same-repository issue number",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			body:      "\r\n\r\nIssue Number: close org/repo#12345\r\n\r\n",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Body:         true,
					Regexp:       issueNumberLineRegexp,
					MissingLabel: "do-not-merge/needs-linked-issue",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR body with cross-repository issue number link",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			body:      "\r\n\r\nIssue Number: close https://github.com/org2/repo2/issues/12345\r\n\r\n",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Body:         true,
					Regexp:       issueNumberLineRegexp,
					MissingLabel: "do-not-merge/needs-linked-issue",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/needs-linked-issue"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR body with same-repository issue number link",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			body:      "\r\n\r\nIssue Number: close https://github.com/org/repo/issues/12345\r\n\r\n",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Body:         true,
					Regexp:       issueNumberLineRegexp,
					MissingLabel: "do-not-merge/needs-linked-issue",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR commits without issue number",
			action:    github.PullRequestActionOpened,
			branch:    "main",
			createdAt: earlierCreatedAt,
			commitMessages: []string{
				"First commit message",
				"Second commit message",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:   true,
					CommitMessage: true,
					Regexp:        issueNumberRegex,
					MissingLabel:  "do-not-merge/invalid-commit-message",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-commit-message"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR commits with issue number",
			action:    github.PullRequestActionOpened,
			branch:    "main",
			createdAt: earlierCreatedAt,
			commitMessages: []string{
				"First commit message\nclose #12345",
				"Second commit message",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:   true,
					CommitMessage: true,
					Regexp:        issueNumberRegex,
					MissingLabel:  "do-not-merge/invalid-commit-message",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR title updated with issue number",
			action:    github.PullRequestActionEdited,
			title:     "[TI-12345] pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:      "PR commit messages or title contain issue number",
			action:    github.PullRequestActionSynchronize,
			title:     "invalid title",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/needs-issue-number",
			},
			commitMessages: []string{
				"First commit message",
				"Second commit message",
				"Third commit message\nclose #12345",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:   true,
					Title:         true,
					Body:          true,
					CommitMessage: true,
					Regexp:        issueNumberRegex,
					MissingLabel:  "do-not-merge/needs-issue-number",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/needs-issue-number"),
			},
		},
		{
			name:      "PR commits with issue number but title is invalid",
			action:    github.PullRequestActionSynchronize,
			title:     "invalid title",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
				"do-not-merge/needs-issue-number",
			},
			commitMessages: []string{
				"First commit message",
				"Second commit message",
				"Third commit message\nclose #12345",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
				{
					PullRequest:   true,
					CommitMessage: true,
					Regexp:        issueNumberRegex,
					MissingLabel:  "do-not-merge/needs-issue-number",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/needs-issue-number"),
			},
		},
		{
			name:      "check issue number and the issue number is correct",
			action:    github.PullRequestActionEdited,
			title:     "[TI-12345][TI-12346] pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:      "check issue number but issue number is wrong",
			action:    github.PullRequestActionEdited,
			title:     "[TI-1234] pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "check issue number but one of issue numbers is wrong",
			action:    github.PullRequestActionEdited,
			title:     "[TI-12345][TI-1234] pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "Labeled the skip label, pass the rule",
			action:    github.PullRequestActionLabeled,
			label:     "skip-issue",
			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
				"skip-issue",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					SkipLabel:    "skip-issue",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:      "Unlabeled the skip label, recheck the rule",
			action:    github.PullRequestActionUnlabeled,
			label:     "skip-issue",
			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					SkipLabel:    "skip-issue",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "Labeled the other label, do not trigger the check",
			action:    github.PullRequestActionLabeled,
			label:     "other",
			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					SkipLabel:    "skip-issue",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "The create time of PR is before start time of the rule",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					StartTime:    &startTime,
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "The create time of PR is after start time of the rule",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: laterCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					StartTime:    &startTime,
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR on the branch that need to be checked",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					Branches:     []string{"main"},
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "PR on the branch that do not need to be checked",
			action:    github.PullRequestActionOpened,
			title:     "pkg: what's changed",
			branch:    "release",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					Branches:     []string{"main"},
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:   "PR authored by trusted user zhang-san doesn't need to be checked",
			action: github.PullRequestActionOpened,

			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					Branches:     []string{"main"},
					TrustedUsers: []string{"zhang-san"},
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:   "PR authored by trusted user zhang-san has do-not-merge label",
			action: github.PullRequestActionOpened,

			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					Branches:     []string{"main"},
					TrustedUsers: []string{"zhang-san"},
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:   "PR authored by trusted user zhang-san need to be checked",
			action: github.PullRequestActionOpened,

			title:     "pkg: what's changed",
			branch:    "main",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					PullRequest:  true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					Branches:     []string{"main"},
					TrustedUsers: []string{"li-si", "wang-wu"},
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
	}

	for _, testcase := range testcases {
		tc := testcase

		commits := make([]github.RepositoryCommit, 0)
		for _, message := range tc.commitMessages {
			commits = append(commits, github.RepositoryCommit{
				Commit: github.GitCommit{
					Message: message,
				},
			})
		}

		labels := make([]github.Label, 0)
		for _, l := range tc.labels {
			labels = append(labels, github.Label{
				Name: l,
			})
		}

		fc := &fakegithub.FakeClient{
			Issues: map[int]*github.Issue{
				12345: {
					Number:      12345,
					PullRequest: nil,
				},
				12346: {
					Number:      12346,
					PullRequest: nil,
				},
				1234: {
					Number:      1234,
					PullRequest: &struct{}{},
				},
			},
			IssueComments:      make(map[int][]github.IssueComment),
			IssueLabelsAdded:   []string{},
			IssueLabelsRemoved: []string{},
			CommitMap: map[string][]github.RepositoryCommit{
				"org/repo#1": commits,
			},
		}

		cfg := &externalplugins.Configuration{}
		cfg.TiCommunityFormatChecker = []externalplugins.TiCommunityFormatChecker{
			{
				Repos:              []string{"org/repo"},
				RequiredMatchRules: tc.requiredMatchRules,
			},
		}

		pe := &github.PullRequestEvent{
			Action: tc.action,
			Number: 1,
			PullRequest: github.PullRequest{
				Title:  tc.title,
				Body:   tc.body,
				Labels: labels,
				Base: github.PullRequestBranch{
					Ref:  tc.branch,
					SHA:  "sha",
					Repo: github.Repo{},
				},
				User: github.User{
					Login: "zhang-san",
				},
				CreatedAt: tc.createdAt,
			},
			Repo: github.Repo{
				Owner: github.User{
					Login: "org",
				},
				Name: "repo",
			},
			Label: github.Label{
				Name: tc.label,
			},
		}
		err := HandlePullRequestEvent(fc, pe, cfg, logrus.WithField("plugin", PluginName))
		if err != nil {
			t.Errorf("For case \"%s\", didn't expect error: %v", tc.name, err)
		}

		sort.Strings(tc.expectAddedLabels)
		sort.Strings(fc.IssueLabelsAdded)
		if !reflect.DeepEqual(tc.expectAddedLabels, fc.IssueLabelsAdded) {
			t.Errorf("For case %s, expected the labels %q to be added, but %q were added",
				tc.name, tc.expectAddedLabels, fc.IssueLabelsAdded)
		}

		sort.Strings(tc.expectDeletedLabels)
		sort.Strings(fc.IssueLabelsRemoved)
		if !reflect.DeepEqual(tc.expectDeletedLabels, fc.IssueLabelsRemoved) {
			t.Errorf("For case %s, expected the labels %q to be deleted, but %q were deleted",
				tc.name, tc.expectDeletedLabels, fc.IssueLabelsRemoved)
		}

		if !tc.shouldComment && len(fc.IssueCommentsAdded) != 0 {
			t.Errorf("unexpected comment %v", fc.IssueCommentsAdded)
		}

		if tc.shouldComment && len(fc.IssueCommentsAdded) == 0 {
			t.Fatalf("expected comments but got none")
		}
	}
}

func TestHandleIssueEvent(t *testing.T) {
	formattedLabel := func(label string) string {
		return fmt.Sprintf("%s/%s#%d:%s", "org", "repo", 1, label)
	}

	earlierCreatedAt, err := time.Parse(time.RFC3339, "2021-10-01T12:00:00Z")
	if err != nil {
		t.Error(err)
		return
	}

	startTime, err := time.Parse(time.RFC3339, "2021-11-01T12:00:00Z")
	if err != nil {
		t.Error(err)
		return
	}

	laterCreatedAt, err := time.Parse(time.RFC3339, "2021-12-01T12:00:00Z")
	if err != nil {
		t.Error(err)
		return
	}

	testcases := []struct {
		name   string
		action github.IssueEventAction
		// label that will be labeled or unlabeled.
		label     string
		title     string
		body      string
		createdAt time.Time
		// labels is the labels existed on the pull request (after the labeled / unlabeled event happened).
		labels             []string
		requiredMatchRules []externalplugins.RequiredMatchRule

		expectAddedLabels   []string
		expectDeletedLabels []string
		shouldComment       bool
	}{
		{
			name:      "Issue title with issue number",
			action:    github.IssueActionOpened,
			title:     "[TI-12345] pkg: what's changed (#999)",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:          true,
					Title:          true,
					Regexp:         issueTitleRegex,
					MissingLabel:   "do-not-merge/invalid-title",
					MissingMessage: "Please fill in the title of the PR according to the prescribed format.",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
			shouldComment:       false,
		},
		{
			name:      "Issue title without issue number",
			action:    github.IssueActionOpened,
			title:     "invalid title",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:          true,
					Title:          true,
					Regexp:         issueTitleRegex,
					MissingLabel:   "do-not-merge/invalid-title",
					MissingMessage: "Please fill in the title of the PR according to the prescribed format.",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
			shouldComment:       true,
		},
		{
			name:      "Issue body without issue number",
			action:    github.IssueActionOpened,
			body:      `Issue Body content`,
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Body:         true,
					Regexp:       issueNumberRegex,
					MissingLabel: "do-not-merge/needs-issue-number",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/needs-issue-number"),
			},
			expectDeletedLabels: []string{},
			shouldComment:       false,
		},
		{
			name:      "Issue body without test task checked",
			action:    github.IssueActionOpened,
			body:      testTaskBody,
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Body:         true,
					Regexp:       testTaskCheckedRegex,
					MissingLabel: "do-not-merge/needs-finish-test-tasks",
				},
			},
			expectAddedLabels: []string{
				formattedLabel("do-not-merge/needs-finish-test-tasks"),
			},
			expectDeletedLabels: []string{},
			shouldComment:       false,
		},
		{
			name:   "Issue body with issue number",
			action: github.IssueActionOpened,
			body: `
			Issue Body content
			close #12345
`,
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Body:         true,
					Regexp:       issueNumberRegex,
					MissingLabel: "do-not-merge/needs-issue-number",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "Issue title updated with issue number",
			action:    github.IssueActionEdited,
			title:     "[TI-12345] pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:      "check issue number but issue number is wrong",
			action:    github.IssueActionEdited,
			title:     "[TI-1234] pkg: what's changed",
			createdAt: earlierCreatedAt,
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "Labeled the skip label, pass the rule",
			action:    github.IssueActionLabeled,
			label:     "skip-issue",
			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
				"skip-issue",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					SkipLabel:    "skip-issue",
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:      "Unlabeled the skip label, recheck the rule",
			action:    github.IssueActionUnlabeled,
			label:     "skip-issue",
			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					SkipLabel:    "skip-issue",
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:      "Labeled the other label, do not trigger the check",
			action:    github.IssueActionLabeled,
			label:     "other",
			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					SkipLabel:    "skip-issue",
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "The create time of issue is before start time of the rule",
			action:    github.IssueActionOpened,
			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					StartTime:    &startTime,
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:      "The create time of issue is after start time of the rule",
			action:    github.IssueActionOpened,
			title:     "pkg: what's changed",
			createdAt: laterCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					StartTime:    &startTime,
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
		{
			name:   "Issue authored by trusted user zhang-san doesn't need to be checked",
			action: github.IssueActionOpened,

			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					TrustedUsers: []string{"zhang-san"},
				},
			},

			expectAddedLabels:   []string{},
			expectDeletedLabels: []string{},
		},
		{
			name:   "Issue authored by trusted user zhang-san and has do-not-merge label",
			action: github.IssueActionOpened,

			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels: []string{
				"do-not-merge/invalid-title",
			},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					TrustedUsers: []string{"zhang-san"},
				},
			},

			expectAddedLabels: []string{},
			expectDeletedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
		},
		{
			name:   "Issue authored by zhang-san who is no trusted by the rule need to be checked",
			action: github.IssueActionOpened,

			title:     "pkg: what's changed",
			createdAt: earlierCreatedAt,
			labels:    []string{},
			requiredMatchRules: []externalplugins.RequiredMatchRule{
				{
					Issue:        true,
					Title:        true,
					Regexp:       issueTitleRegex,
					MissingLabel: "do-not-merge/invalid-title",
					TrustedUsers: []string{"li-si", "wang-wu"},
				},
			},

			expectAddedLabels: []string{
				formattedLabel("do-not-merge/invalid-title"),
			},
			expectDeletedLabels: []string{},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			tc := testcase

			labels := make([]github.Label, 0)
			for _, l := range tc.labels {
				labels = append(labels, github.Label{
					Name: l,
				})
			}

			fc := &fakegithub.FakeClient{
				Issues: map[int]*github.Issue{
					12345: {
						Number:      12345,
						PullRequest: nil,
					},
					1234: {
						Number:      1234,
						PullRequest: &struct{}{},
					},
				},
				IssueComments:      make(map[int][]github.IssueComment),
				IssueLabelsAdded:   []string{},
				IssueLabelsRemoved: []string{},
			}

			cfg := &externalplugins.Configuration{}
			cfg.TiCommunityFormatChecker = []externalplugins.TiCommunityFormatChecker{
				{
					Repos:              []string{"org/repo"},
					RequiredMatchRules: tc.requiredMatchRules,
				},
			}

			ie := &github.IssueEvent{
				Action: tc.action,
				Issue: github.Issue{
					Number:    1,
					Title:     tc.title,
					Body:      tc.body,
					User:      github.User{Login: "zhang-san"},
					CreatedAt: tc.createdAt,
					Labels:    labels,
				},
				Repo: github.Repo{
					Owner: github.User{
						Login: "org",
					},
					Name: "repo",
				},
				Label: github.Label{
					Name: tc.label,
				},
			}
			err := HandleIssueEvent(fc, ie, cfg, logrus.WithField("plugin", PluginName))
			if err != nil {
				t.Errorf("For case %s, didn't expect error: %v", tc.name, err)
			}

			sort.Strings(tc.expectAddedLabels)
			sort.Strings(fc.IssueLabelsAdded)
			if !reflect.DeepEqual(tc.expectAddedLabels, fc.IssueLabelsAdded) {
				t.Errorf("For case \"%s\", expected the labels %q to be added, but %q were added",
					tc.name, tc.expectAddedLabels, fc.IssueLabelsAdded)
			}

			sort.Strings(tc.expectDeletedLabels)
			sort.Strings(fc.IssueLabelsRemoved)
			if !reflect.DeepEqual(tc.expectDeletedLabels, fc.IssueLabelsRemoved) {
				t.Errorf("For case %s, expected the labels %q to be deleted, but %q were deleted",
					tc.name, tc.expectDeletedLabels, fc.IssueLabelsRemoved)
			}

			if !tc.shouldComment && len(fc.IssueCommentsAdded) != 0 {
				t.Errorf("unexpected comment %v", fc.IssueCommentsAdded)
			}

			if tc.shouldComment && len(fc.IssueCommentsAdded) == 0 {
				t.Fatalf("expected comments but got none")
			}
		})
	}
}

func TestHelpProvider(t *testing.T) {
	enabledRepos := []config.OrgRepo{
		{Org: "org1", Repo: "repo"},
		{Org: "org2", Repo: "repo"},
	}
	cases := []struct {
		name               string
		config             *externalplugins.Configuration
		enabledRepos       []config.OrgRepo
		err                bool
		configInfoIncludes []string
		configInfoExcludes []string
	}{
		{
			name:               "Empty config",
			config:             &externalplugins.Configuration{},
			enabledRepos:       enabledRepos,
			configInfoExcludes: []string{"matched by regex"},
		},
		{
			name: "All configs enabled",
			config: &externalplugins.Configuration{
				TiCommunityFormatChecker: []externalplugins.TiCommunityFormatChecker{
					{
						Repos: []string{"org2/repo"},
						RequiredMatchRules: []externalplugins.RequiredMatchRule{
							{
								PullRequest: true,
								Title:       true,
								Regexp:      issueTitleRegex,
							},
						},
					},
				},
			},
			enabledRepos:       enabledRepos,
			configInfoIncludes: []string{"matched by regex"},
		},
	}
	for _, testcase := range cases {
		tc := testcase
		t.Run(tc.name, func(t *testing.T) {
			epa := &externalplugins.ConfigAgent{}
			epa.Set(tc.config)

			helpProvider := HelpProvider(epa)
			pluginHelp, err := helpProvider(tc.enabledRepos)
			if err != nil && !tc.err {
				t.Fatalf("helpProvider error: %v", err)
			}
			for _, msg := range tc.configInfoExcludes {
				if strings.Contains(pluginHelp.Config["org2/repo"], msg) {
					t.Fatalf("helpProvider.Config error mismatch: got %v, but didn't want it", msg)
				}
			}
			for _, msg := range tc.configInfoIncludes {
				if !strings.Contains(pluginHelp.Config["org2/repo"], msg) {
					t.Fatalf("helpProvider.Config error mismatch: didn't get %v, but wanted it", msg)
				}
			}
		})
	}
}

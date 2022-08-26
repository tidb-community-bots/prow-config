package cherrypicker

import (
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/test-infra/prow/git/localgit"
	"k8s.io/test-infra/prow/github"

	"github.com/ti-community-infra/tichi/internal/pkg/externalplugins"
)

func (f *fghc) IsCollaborator(org, repo, user string) (bool, error) {
	if user == "a_user_that_will_make_a_fake_error_when_judge" {
		return false, errors.New("fake error")
	}

	return sets.NewString(f.collaborators...).Has(user), nil
}

func (f *fghc) AddCollaborator(org, repo, user, permission string) error {
	if user == "a_user_that_will_make_a_fake_error_when_add" {
		return errors.New("fake error")
	}

	f.collaborators = append(f.collaborators, user)
	return nil
}
func TestInviteIC(t *testing.T) {
	lg, c, err := localgit.NewV2()
	if err != nil {
		t.Fatalf("Making localgit: %v", err)
	}
	t.Cleanup(func() {
		if err := lg.Clean(); err != nil {
			t.Errorf("Cleaning up localgit: %v", err)
		}
		if err := c.Clean(); err != nil {
			t.Errorf("Cleaning up client: %v", err)
		}
	})
	if err := lg.MakeFakeRepo("foo", "bar"); err != nil {
		t.Fatalf("Making fake repo: %v", err)
	}
	if err := lg.AddCommit("foo", "bar", initialFiles); err != nil {
		t.Fatalf("Adding initial commit: %v", err)
	}

	expectedBranches := []string{"stage", "release-1.5"}
	for _, branch := range expectedBranches {
		if err := lg.CheckoutNewBranch("foo", "bar", branch); err != nil {
			t.Fatalf("Checking out pull branch: %v", err)
		}
	}

	botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}
	getSecret := func() []byte {
		return []byte("sha=abcdefg")
	}

	getGithubToken := func() []byte {
		return []byte("token")
	}

	cfg := &externalplugins.Configuration{}
	cfg.TiCommunityCherrypicker = []externalplugins.TiCommunityCherrypicker{
		{
			Repos:             []string{"foo/bar"},
			LabelPrefix:       "cherrypick/",
			PickedLabelPrefix: "type/cherrypick-for-",
		},
	}
	ca := &externalplugins.ConfigAgent{}
	ca.Set(cfg)

	tests := []struct {
		name          string
		collaborators []string
		commentUser   string
		wantError     bool
		wantHad       bool
	}{
		{
			name:          "invite when was a collabtor",
			collaborators: []string{"wiseguy"},
			commentUser:   "wiseguy",
			wantError:     false,
			wantHad:       true,
		},
		{
			name:          "invite when not a collabtor",
			collaborators: nil,
			commentUser:   "wiseguy",
			wantError:     false,
			wantHad:       true,
		},
		{
			name:          "invite error when judge",
			collaborators: nil,
			commentUser:   "a_user_that_will_make_a_fake_error_when_judge",
			wantError:     true,
			wantHad:       false,
		},
		{
			name:          "invite error when add",
			collaborators: nil,
			commentUser:   "a_user_that_will_make_a_fake_error_when_add",
			wantError:     true,
			wantHad:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ghc := &fghc{
				pr: &github.PullRequest{
					Base:      github.PullRequestBranch{Ref: "master"},
					Number:    2,
					Merged:    true,
					Title:     "This is a fix for X",
					Body:      body,
					Assignees: []github.User{{Login: "user2"}},
				},
				isMember:      true,
				patch:         patch,
				collaborators: tt.collaborators,
			}
			ic := github.IssueCommentEvent{
				Action: github.IssueCommentActionCreated,
				Repo: github.Repo{
					Owner:    github.User{Login: "foo"},
					Name:     "bar",
					FullName: "foo/bar",
				},
				Issue: github.Issue{Number: 2, State: "closed", PullRequest: &struct{}{}},
				Comment: github.IssueComment{
					User: github.User{Login: tt.commentUser},
					Body: "/cherry-pick-invite",
				},
			}
			s := &Server{
				BotUser:                botUser,
				GitClient:              c,
				ConfigAgent:            ca,
				Push:                   func(forkName, newBranch string, force bool) error { return nil },
				GitHubClient:           ghc,
				WebhookSecretGenerator: getSecret,
				GitHubTokenGenerator:   getGithubToken,
				Log:                    logrus.StandardLogger().WithField("client", "cherrypicker"),
				Repos:                  []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},
			}

			if err := s.handleIssueComment(logrus.NewEntry(logrus.StandardLogger()),
				ic); (err != nil) != tt.wantError {
				t.Fatalf("got error: %v, expected error: %v", err, tt.wantError)
			}

			hasErrTpl := "Expected collaborators contains %s, got %v"
			if !tt.wantHad {
				hasErrTpl = "Expected collaborators not contains %s, got %v"
			}
			if sets.NewString(ghc.collaborators...).Has(ic.Comment.User.Login) != tt.wantHad {
				t.Fatalf(hasErrTpl, ic.Comment.User.Login, ghc.collaborators)
			}
		})
	}
}

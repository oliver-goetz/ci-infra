/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus"

	"sigs.k8s.io/prow/pkg/git/localgit"
	v2 "sigs.k8s.io/prow/pkg/git/v2"
	"sigs.k8s.io/prow/pkg/github"
)

var commentFormat = "%s/%s#%d %s"

var fakePR prNumberGenerator

type fghc struct {
	sync.Mutex
	isMember bool

	diff       []byte
	patch      []byte
	comments   []string
	prs        []github.PullRequest
	prComments []github.IssueComment
	prLabels   []github.Label
	orgMembers []github.TeamMember
	issues     []github.Issue
}

func (f *fghc) AddLabel(_, _ string, number int, label string) error {
	f.Lock()
	defer f.Unlock()
	for i := range f.prs {
		if number == f.prs[i].Number {
			f.prs[i].Labels = append(f.prs[i].Labels, github.Label{Name: label})
		}
	}
	return nil
}

func (f *fghc) AssignIssue(_, _ string, number int, logins []string) error {
	var users []github.User
	for _, login := range logins {
		users = append(users, github.User{Login: login})
	}

	f.Lock()
	for i := range f.prs {
		if number == f.prs[i].Number {
			f.prs[i].Assignees = append(f.prs[i].Assignees, users...)
		}
	}
	defer f.Unlock()
	return nil
}

func (f *fghc) GetPullRequest(_, _ string, num int) (*github.PullRequest, error) {
	f.Lock()
	defer f.Unlock()
	for _, pr := range f.prs {
		if pr.Number == num {
			return &pr, nil
		}
	}
	return nil, fmt.Errorf("PR #%d not found", num)
}

func (f *fghc) GetPullRequestDiff(_, _ string, _ int) ([]byte, error) {
	f.Lock()
	defer f.Unlock()
	return f.diff, nil
}

func (f *fghc) GetPullRequestPatch(_, _ string, _ int) ([]byte, error) {
	f.Lock()
	defer f.Unlock()
	return f.patch, nil
}

func (f *fghc) GetPullRequests(_, _ string) ([]github.PullRequest, error) {
	f.Lock()
	defer f.Unlock()
	return f.prs, nil
}

func (f *fghc) EditPullRequest(_, _ string, num int, pullRequest *github.PullRequest) (*github.PullRequest, error) {
	f.Lock()
	defer f.Unlock()
	for i, pr := range f.prs {
		if pr.Number == num {
			f.prs[i] = *pullRequest
			return pullRequest, nil
		}
	}
	return nil, fmt.Errorf("PR #%d not found", num)
}

func (f *fghc) CreateComment(org, repo string, number int, comment string) error {
	f.Lock()
	defer f.Unlock()
	f.comments = append(f.comments, fmt.Sprintf(commentFormat, org, repo, number, comment))
	return nil
}

func (f *fghc) IsMember(_, _ string) (bool, error) {
	f.Lock()
	defer f.Unlock()
	return f.isMember, nil
}

func (f *fghc) IsCollaborator(_, _, _ string) (bool, error) {
	f.Lock()
	defer f.Unlock()
	return f.isMember, nil
}

func (f *fghc) GetRepo(_, _ string) (github.FullRepo, error) {
	f.Lock()
	defer f.Unlock()
	return github.FullRepo{}, nil
}

func (f *fghc) EnsureFork(_, _, repo string) (string, error) {
	if repo == "changeme" {
		return "changed", nil
	}
	if repo == "error" {
		return repo, errors.New("errors")
	}
	return repo, nil
}

var expectedFmt = `title=%q body=%q head=%s base=%s labels=%v`

func prToString(pr github.PullRequest) string {
	var labels []string
	for _, label := range pr.Labels {
		labels = append(labels, label.Name)
	}
	return fmt.Sprintf(expectedFmt, pr.Title, pr.Body, pr.Head.Ref, pr.Base.Ref, labels)
}

func (f *fghc) CreateIssue(_, _, title, body string, _ int, labels, assignees []string) (int, error) {
	f.Lock()
	defer f.Unlock()

	var ghLabels []github.Label
	var ghAssignees []github.User

	var num int
	for _, issue := range f.issues {
		if issue.Number > num {
			num = issue.Number
		}
	}
	num++

	for _, label := range labels {
		ghLabels = append(ghLabels, github.Label{Name: label})
	}

	for _, assignee := range assignees {
		ghAssignees = append(ghAssignees, github.User{Login: assignee})
	}

	f.issues = append(f.issues, github.Issue{
		Title:     title,
		Body:      body,
		Number:    num,
		Labels:    ghLabels,
		Assignees: ghAssignees,
	})

	return num, nil
}

func (f *fghc) CreatePullRequest(_, _, title, body, head, base string, _ bool) (int, error) {
	f.Lock()
	defer f.Unlock()

	var num int
	for _, pr := range f.prs {
		if pr.Number > num {
			num = pr.Number
		}
	}
	num++

	f.prs = append(f.prs, github.PullRequest{
		Title:  title,
		Body:   body,
		Number: num,
		Head:   github.PullRequestBranch{Ref: head},
		Base:   github.PullRequestBranch{Ref: base},
	})
	return num, nil
}

func (f *fghc) ListIssueComments(_, _ string, _ int) ([]github.IssueComment, error) {
	f.Lock()
	defer f.Unlock()
	return f.prComments, nil
}

func (f *fghc) GetIssueLabels(_, _ string, _ int) ([]github.Label, error) {
	f.Lock()
	defer f.Unlock()
	return f.prLabels, nil
}

func (f *fghc) ListOrgMembers(_, role string) ([]github.TeamMember, error) {
	f.Lock()
	defer f.Unlock()
	if role != "all" {
		return nil, fmt.Errorf("all is only supported role, not: %s", role)
	}
	return f.orgMembers, nil
}

func (f *fghc) ListCollaborators(_, _ string) ([]github.User, error) {
	f.Lock()
	defer f.Unlock()
	githubUsers := []github.User{}
	for _, user := range f.orgMembers {
		githubUser := github.User{Login: user.Login}
		githubUsers = append(githubUsers, githubUser)
	}
	return githubUsers, nil
}

func (f *fghc) CreateFork(_, repo string) (string, error) {
	return repo, nil
}

var initialFiles = map[string][]byte{
	"bar.go": []byte(`// Package bar does an interesting thing.
package bar

// Foo does a thing.
func Foo(wow int) int {
	return 42 + wow
}
`),
}

var patch = []byte(`From af468c9e69dfdf39db591f1e3e8de5b64b0e62a2 Mon Sep 17 00:00:00 2001
From: Wise Guy <wise@guy.com>
Date: Thu, 19 Oct 2017 15:14:36 +0200
Subject: [PATCH] Update magic number

---
 bar.go | 3 ++-
 1 file changed, 2 insertions(+), 1 deletion(-)

diff --git a/bar.go b/bar.go
index 1ea52dc..5bd70a9 100644
--- a/bar.go
+++ b/bar.go
@@ -3,5 +3,6 @@ package bar

 // Foo does a thing.
 func Foo(wow int) int {
-	return 42 + wow
+	// Needs to be 49 because of a reason.
+	return 49 + wow
 }
`)

var body = "This PR updates the magic number.\n\n```feature developer\nUpdate the magic number from 42 to 49\n```"

func makeFakeRepoWithCommit(clients localgit.Clients, t *testing.T) (*localgit.LocalGit, v2.ClientFactory) {
	lg, c, err := clients()
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
	return lg, c
}

func TestCherryPickICV2(t *testing.T) {
	t.Parallel()
	testCherryPickIC(localgit.NewV2, t)
}

func testCherryPickIC(clients localgit.Clients, t *testing.T) {
	iNumber := fakePR.GetPRNumber()
	lg, c := makeFakeRepoWithCommit(clients, t)
	if err := lg.CheckoutNewBranch("foo", "bar", "stage"); err != nil {
		t.Fatalf("Checking out pull branch: %v", err)
	}

	ghc := &fghc{
		prs: []github.PullRequest{
			{
				Number: iNumber,
				Base: github.PullRequestBranch{
					Ref: "master",
				},
				User:   github.User{Login: "foo-author"},
				Merged: true,
				Title:  "This is a fix for X",
				Body:   body,
			},
		},
		isMember: true,
		patch:    patch,
	}
	ic := github.IssueCommentEvent{
		Action: github.IssueCommentActionCreated,
		Repo: github.Repo{
			Owner: github.User{
				Login: "foo",
			},
			Name:     "bar",
			FullName: "foo/bar",
		},
		Issue: github.Issue{
			Number:      iNumber,
			State:       "closed",
			PullRequest: &struct{}{},
		},
		Comment: github.IssueComment{
			User: github.User{
				Login: "wiseguy",
			},
			Body: "/cherrypick stage",
		},
	}

	botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}
	expectedTitle := "[stage] This is a fix for X"
	expectedBody := fmt.Sprintf("This is an automated cherry-pick of #%d\n\n/assign wiseguy\n\n```feature developer github.com/foo/bar #%d @foo-author\nUpdate the magic number from 42 to 49\n```", iNumber, iNumber+1)
	expectedBase := "stage"
	expectedHead := fmt.Sprintf(botUser.Login+":"+cherryPickBranchFmt, iNumber, expectedBase)
	expectedLabels := []string{}
	expected := fmt.Sprintf(expectedFmt, expectedTitle, expectedBody, expectedHead, expectedBase, expectedLabels)

	getSecret := func() []byte {
		return []byte("sha=abcdefg")
	}

	s := &Server{
		botUser:        botUser,
		gc:             c,
		push:           func(_, _ string, _ bool) error { return nil },
		ghc:            ghc,
		tokenGenerator: getSecret,
		log:            logrus.StandardLogger().WithField("client", "cherrypicker"),
		repos:          []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},

		prowAssignments: true,
	}

	if err := s.handleIssueComment(logrus.NewEntry(logrus.StandardLogger()), ic); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := prToString(ghc.prs[1])
	if got != expected {
		t.Errorf("Expected (%d):\n%s\nGot (%d):\n%+v\n", len(expected), expected, len(got), got)
	}
}

func TestCherryPickPRV2(t *testing.T) {
	t.Parallel()
	testCherryPickPR(localgit.NewV2, t)
}

func testCherryPickPR(clients localgit.Clients, t *testing.T) {
	prNumber := fakePR.GetPRNumber()
	lg, c := makeFakeRepoWithCommit(clients, t)
	expectedBranches := []string{"release-1.5", "release-1.6", "release-1.8", "release-1.3", "release-1.12"}
	for _, branch := range expectedBranches {
		if err := lg.CheckoutNewBranch("foo", "bar", branch); err != nil {
			t.Fatalf("Checking out pull branch: %v", err)
		}
	}
	if err := lg.CheckoutNewBranch("foo", "bar", fmt.Sprintf("cherry-pick-%d-to-release-1.5", prNumber)); err != nil {
		t.Fatalf("Checking out existing PR branch: %v", err)
	}

	ghc := &fghc{
		orgMembers: []github.TeamMember{
			{
				Login: "approver",
			},
			{
				Login: "merge-bot",
			},
		},
		prComments: []github.IssueComment{
			{
				User: github.User{
					Login: "developer",
				},
				Body: "a review comment",
			},
			{
				User: github.User{
					Login: "approver",
				},
				Body: "/cherrypick release-1.5\r\n/cherrypick release-1.8",
			},
			{
				User: github.User{
					Login: "approver",
				},
				Body: "/cherrypick release-1.6",
			},
			{
				User: github.User{
					Login: "approver",
				},
				Body: "/cherrypick release-1.3 release-1.2",
			},
			{
				User: github.User{
					Login: "approver",
				},
				Body: "/cherrypick release-1.12 release-1.11 release-1.10 release-1.9",
			},
			{
				User: github.User{
					Login: "fan",
				},
				Body: "/cherrypick release-1.7",
			},
			{
				User: github.User{
					Login: "approver",
				},
				Body: "/approve",
			},
			{
				User: github.User{
					Login: "merge-bot",
				},
				Body: "Automatic merge from submit-queue.",
			},
		},
		prs: []github.PullRequest{
			{
				Title: "[release-1.5] This is a fix for Y",
				Body:  fmt.Sprintf("This is an automated cherry-pick of #%d", prNumber),
				Base: github.PullRequestBranch{
					Ref: "release-1.5",
				},
				Head: github.PullRequestBranch{
					Ref: fmt.Sprintf("ci-robot:cherry-pick-%d-to-release-1.5", prNumber),
				},
			},
		},
		isMember: true,
		patch:    patch,
	}
	pr := github.PullRequestEvent{
		Action: github.PullRequestActionClosed,
		PullRequest: github.PullRequest{
			Base: github.PullRequestBranch{
				Ref: "master",
				Repo: github.Repo{
					Owner: github.User{
						Login: "foo",
					},
					Name: "bar",
				},
			},
			Number:   prNumber,
			Merged:   true,
			MergeSHA: new(string),
			Title:    "This is a fix for Y",
		},
	}

	botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}

	getSecret := func() []byte {
		return []byte("sha=abcdefg")
	}

	s := &Server{
		botUser:        botUser,
		gc:             c,
		push:           func(_, _ string, _ bool) error { return nil },
		ghc:            ghc,
		tokenGenerator: getSecret,
		log:            logrus.StandardLogger().WithField("client", "cherrypicker"),
		repos:          []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},

		prowAssignments: false,
	}

	if err := s.handlePullRequest(logrus.NewEntry(logrus.StandardLogger()), pr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var expectedFn = func(branch string) string {
		expectedTitle := fmt.Sprintf("[%s] This is a fix for Y", branch)
		expectedBody := fmt.Sprintf("This is an automated cherry-pick of #%d", prNumber)
		if branch == "release-1.3" {
			expectedBody = fmt.Sprintf("%s\n\n/cherrypick release-1.2", expectedBody)
		}
		if branch == "release-1.12" {
			expectedBody = fmt.Sprintf("%s\n\n/cherrypick release-1.11 release-1.10 release-1.9", expectedBody)
		}
		expectedHead := fmt.Sprintf(botUser.Login+":"+cherryPickBranchFmt, prNumber, branch)
		expectedLabels := s.labels
		return fmt.Sprintf(expectedFmt, expectedTitle, expectedBody, expectedHead, branch, expectedLabels)
	}

	if len(ghc.prs) != len(expectedBranches) {
		t.Fatalf("Expected %d PRs, got %d", len(expectedBranches), len(ghc.prs))
	}

	expectedPrs := make(map[string]string)
	for _, branch := range expectedBranches {
		expectedPrs[expectedFn(branch)] = branch
	}
	seenBranches := make(map[string]struct{})
	for _, p := range ghc.prs {
		pr := prToString(p)
		branch, present := expectedPrs[pr]
		if !present {
			t.Errorf("Unexpected PR:\n%s\nExpected to target one of the following branches: %v\n", pr, expectedBranches)
		}
		seenBranches[branch] = struct{}{}
	}
	if len(seenBranches) != len(expectedBranches) {
		t.Fatalf("Expected to see PRs for %d branches, got %d (%v)", len(expectedBranches), len(seenBranches), seenBranches)
	}
}

func TestCherryPickOfCherryPickPRV2(t *testing.T) {
	t.Parallel()
	testCherryPickOfCherryPickPR(localgit.NewV2, t)
}

// testCherryPickOfCherryPickPR checks that the omitBaseBranchFromTitle
// function works as intended when the user performs a cherry-pick from
// a branch that's already a cherry-pick branch
func testCherryPickOfCherryPickPR(clients localgit.Clients, t *testing.T) {
	prNumber := fakePR.GetPRNumber()
	lg, c := makeFakeRepoWithCommit(clients, t)
	expectedBranches := []string{"release-1.5", "release-1.6", "release-1.8"}
	for _, branch := range expectedBranches {
		if err := lg.CheckoutNewBranch("foo", "bar", branch); err != nil {
			t.Fatalf("Checking out pull branch: %v", err)
		}
	}

	ghc := &fghc{
		orgMembers: []github.TeamMember{
			{
				Login: "approver",
			},
		},
		prComments: []github.IssueComment{
			{
				User: github.User{
					Login: "approver",
				},
				Body: "/cherrypick release-1.8",
			},
		},
		prs:      []github.PullRequest{},
		isMember: true,
		patch:    patch,
	}

	pr := github.PullRequestEvent{
		Action: github.PullRequestActionClosed,
		PullRequest: github.PullRequest{
			Base: github.PullRequestBranch{
				Ref: "master",
				Repo: github.Repo{
					Owner: github.User{
						Login: "foo",
					},
					Name: "bar",
				},
			},
			Number:   prNumber,
			Merged:   true,
			MergeSHA: new(string),
			Title:    "This is a fix for Y",
		},
	}

	botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}

	getSecret := func() []byte {
		return []byte("sha=abcdefg")
	}

	s := &Server{
		botUser:        botUser,
		gc:             c,
		push:           func(_, _ string, _ bool) error { return nil },
		ghc:            ghc,
		tokenGenerator: getSecret,
		log:            logrus.StandardLogger().WithField("client", "cherrypicker"),
		repos:          []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},

		prowAssignments: false,
	}

	// Cherry pick master -> release-1.8
	if err := s.handlePullRequest(logrus.NewEntry(logrus.StandardLogger()), pr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cherry pick release-1.8 -> release-1.6
	pr.PullRequest.Base.Ref = "release-1.8"
	pr.PullRequest.Title = "[release-1.8] This is a fix for Y"
	ghc.prComments[0].Body = "/cherrypick release-1.6"
	if err := s.handlePullRequest(logrus.NewEntry(logrus.StandardLogger()), pr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cherry pick release-1.6 -> release-1.5
	pr.PullRequest.Base.Ref = "release-1.6"
	pr.PullRequest.Title = "[release-1.6] This is a fix for Y"
	ghc.prComments[0].Body = "/cherrypick release-1.5"
	if err := s.handlePullRequest(logrus.NewEntry(logrus.StandardLogger()), pr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var expectedFn = func(branch string) string {
		expectedTitle := fmt.Sprintf("[%s] This is a fix for Y", branch)
		expectedBody := fmt.Sprintf("This is an automated cherry-pick of #%d", prNumber)
		expectedHead := fmt.Sprintf(botUser.Login+":"+cherryPickBranchFmt, prNumber, branch)
		expectedLabels := s.labels
		return fmt.Sprintf(expectedFmt, expectedTitle, expectedBody, expectedHead, branch, expectedLabels)
	}

	if len(ghc.prs) != len(expectedBranches) {
		t.Fatalf("Expected %d PRs, got %d", len(expectedBranches), len(ghc.prs))
	}

	expectedPrs := make(map[string]string)
	for _, branch := range expectedBranches {
		expectedPrs[expectedFn(branch)] = branch
	}
	seenBranches := make(map[string]struct{})
	for _, p := range ghc.prs {
		pr := prToString(p)
		branch, present := expectedPrs[pr]
		if !present {
			t.Errorf("Unexpected PR:\n%s\nExpected to target one of the following branches: %v\n", pr, expectedBranches)
		}
		seenBranches[branch] = struct{}{}
	}
	if len(seenBranches) != len(expectedBranches) {
		t.Fatalf("Expected to see PRs for %d branches, got %d (%v)", len(expectedBranches), len(seenBranches), seenBranches)
	}
}

func TestCherryPickPRWithLabelsV2(t *testing.T) {
	t.Parallel()
	testCherryPickPRWithLabels(localgit.NewV2, t)
}

func testCherryPickPRWithLabels(clients localgit.Clients, t *testing.T) {
	prNumber := fakePR.GetPRNumber()
	lg, c := makeFakeRepoWithCommit(clients, t)
	if err := lg.CheckoutNewBranch("foo", "bar", "release-1.5"); err != nil {
		t.Fatalf("Checking out pull branch: %v", err)
	}
	if err := lg.CheckoutNewBranch("foo", "bar", "release-1.6"); err != nil {
		t.Fatalf("Checking out pull branch: %v", err)
	}

	pr := func(evt github.PullRequestEventAction) github.PullRequestEvent {
		return github.PullRequestEvent{
			Action: evt,
			PullRequest: github.PullRequest{
				User: github.User{
					Login: "developer",
				},
				Base: github.PullRequestBranch{
					Ref: "master",
					Repo: github.Repo{
						Owner: github.User{
							Login: "foo",
						},
						Name: "bar",
					},
				},
				Number:   prNumber,
				Merged:   true,
				MergeSHA: new(string),
				Title:    "This is a fix for Y",
			},
		}
	}

	events := []github.PullRequestEventAction{github.PullRequestActionClosed, github.PullRequestActionLabeled}

	botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}

	getSecret := func() []byte {
		return []byte("sha=abcdefg")
	}

	testCases := []struct {
		name        string
		labelPrefix string
		prLabels    []github.Label
		prComments  []github.IssueComment
	}{
		{
			name:        "Default label prefix",
			labelPrefix: defaultLabelPrefix,
			prLabels: []github.Label{
				{
					Name: "cherrypick/release-1.5",
				},
				{
					Name: "cherrypick/release-1.6",
				},
				{
					Name: "cherrypick/release-1.7",
				},
			},
		},
		{
			name:        "Custom label prefix",
			labelPrefix: "needs-cherry-pick-",
			prLabels: []github.Label{
				{
					Name: "needs-cherry-pick-release-1.5",
				},
				{
					Name: "needs-cherry-pick-release-1.6",
				},
				{
					Name: "needs-cherry-pick-release-1.7",
				},
			},
		},
		{
			name:        "No labels, label gets ignored",
			labelPrefix: "needs-cherry-pick-",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, evt := range events {
				t.Run(string(evt), func(t *testing.T) {
					ghc := &fghc{
						orgMembers: []github.TeamMember{
							{
								Login: "approver",
							},
							{
								Login: "merge-bot",
							},
							{
								Login: "developer",
							},
						},
						prComments: []github.IssueComment{
							{
								User: github.User{
									Login: "developer",
								},
								Body: "a review comment",
							},
							{
								User: github.User{
									Login: "approver",
								},
								Body: "/cherrypick release-1.5\r",
							},
						},
						prLabels: tc.prLabels,
						isMember: true,
						patch:    patch,
					}

					s := &Server{
						botUser:        botUser,
						gc:             c,
						push:           func(_, _ string, _ bool) error { return nil },
						ghc:            ghc,
						tokenGenerator: getSecret,
						log:            logrus.StandardLogger().WithField("client", "cherrypicker"),
						repos:          []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},

						labels:          []string{"cla: yes"},
						prowAssignments: false,
						labelPrefix:     tc.labelPrefix,
					}

					if err := s.handlePullRequest(logrus.NewEntry(logrus.StandardLogger()), pr(evt)); err != nil {
						t.Fatalf("unexpected error: %v", err)
					}

					expectedFn := func(branch string) string {
						expectedTitle := fmt.Sprintf("[%s] This is a fix for Y", branch)
						expectedBody := fmt.Sprintf("This is an automated cherry-pick of #%d", prNumber)
						expectedHead := fmt.Sprintf(botUser.Login+":"+cherryPickBranchFmt, prNumber, branch)
						expectedLabels := s.labels
						return fmt.Sprintf(expectedFmt, expectedTitle, expectedBody, expectedHead, branch, expectedLabels)
					}

					expectedPRs := 2
					if len(tc.prLabels) == 0 {
						if evt == github.PullRequestActionLabeled {
							expectedPRs = 0
						} else {
							expectedPRs = 1
						}
					}
					if len(ghc.prs) != expectedPRs {
						t.Errorf("Expected %d PRs, got %d", expectedPRs, len(ghc.prs))
					}

					expectedBranches := []string{"release-1.5", "release-1.6"}
					seenBranches := make(map[string]struct{})
					for _, p := range ghc.prs {
						pr := prToString(p)
						if pr != expectedFn("release-1.5") && pr != expectedFn("release-1.6") {
							t.Errorf("Unexpected PR:\n%s\nExpected to target one of the following branches: %v", pr, expectedBranches)
						}
						if pr == expectedFn("release-1.5") {
							seenBranches["release-1.5"] = struct{}{}
						}
						if pr == expectedFn("release-1.6") {
							seenBranches["release-1.6"] = struct{}{}
						}
					}
					if len(seenBranches) != expectedPRs {
						t.Fatalf("Expected to see PRs for %d branches, got %d (%v)", expectedPRs, len(seenBranches), seenBranches)
					}
				})
			}
		})
	}
}

func TestCherryPickCreateIssue(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		org       string
		repo      string
		title     string
		body      string
		prNum     int
		labels    []string
		assignees []string
	}{
		{
			org:       "istio",
			repo:      "istio",
			title:     "brand new feature",
			body:      "automated cherry-pick",
			prNum:     2190,
			labels:    nil,
			assignees: []string{"clarketm"},
		},
		{
			org:       "kubernetes",
			repo:      "kubernetes",
			title:     "alpha feature",
			body:      "automated cherry-pick",
			prNum:     3444,
			labels:    []string{"new", "1.18"},
			assignees: nil,
		},
	}

	errMsg := func(field string) string {
		return fmt.Sprintf("GH issue %q does not match: \nexpected: \"%%v\" \nactual: \"%%v\"", field)
	}

	for _, tc := range testCases {

		ghc := &fghc{}

		s := &Server{
			ghc: ghc,
		}

		if err := s.createIssue(logrus.WithField("test", t.Name()), tc.org, tc.repo, tc.title, tc.body, tc.prNum, nil, tc.labels, tc.assignees); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(ghc.issues) < 1 {
			t.Fatalf("Expected 1 GH issue to be created but got: %d", len(ghc.issues))
		}

		ghIssue := ghc.issues[len(ghc.issues)-1]

		if tc.title != ghIssue.Title {
			t.Fatalf(errMsg("title"), tc.title, ghIssue.Title)
		}

		if tc.body != ghIssue.Body {
			t.Fatalf(errMsg("body"), tc.title, ghIssue.Title)
		}

		if len(ghc.issues) != ghIssue.Number {
			t.Fatalf(errMsg("number"), len(ghc.issues), ghIssue.Number)
		}

		var actualAssignees []string
		for _, assignee := range ghIssue.Assignees {
			actualAssignees = append(actualAssignees, assignee.Login)
		}

		if !reflect.DeepEqual(tc.assignees, actualAssignees) {
			t.Fatalf(errMsg("assignees"), tc.assignees, actualAssignees)
		}

		var actualLabels []string
		for _, label := range ghIssue.Labels {
			actualLabels = append(actualLabels, label.Name)
		}

		if !reflect.DeepEqual(tc.labels, actualLabels) {
			t.Fatalf(errMsg("labels"), tc.labels, actualLabels)
		}

		cpFormat := fmt.Sprintf(commentFormat, tc.org, tc.repo, tc.prNum, "In response to a cherrypick label: %s")
		expectedComment := fmt.Sprintf(cpFormat, fmt.Sprintf("new issue created for failed cherrypick: #%d", ghIssue.Number))
		actualComment := ghc.comments[len(ghc.comments)-1]

		if expectedComment != actualComment {
			t.Fatalf(errMsg("comment"), expectedComment, actualComment)
		}

	}
}

func TestCherryPickPRAssignmentsV2(t *testing.T) {
	t.Parallel()
	testCherryPickPRAssignments(localgit.NewV2, t)
}

func testCherryPickPRAssignments(clients localgit.Clients, t *testing.T) {
	iNumber := fakePR.GetPRNumber()
	for _, prowAssignments := range []bool{true, false} {
		lg, c := makeFakeRepoWithCommit(clients, t)
		if err := lg.CheckoutNewBranch("foo", "bar", "stage"); err != nil {
			t.Fatalf("Checking out pull branch: %v", err)
		}

		user := github.User{
			Login: "wiseguy",
		}
		ghc := &fghc{
			prs: []github.PullRequest{
				{
					Number: iNumber,
					Base: github.PullRequestBranch{
						Ref: "master",
					},
					Merged: true,
					Title:  "This is a fix for X",
					Body:   body,
				},
			},
			isMember: true,
			patch:    patch,
		}
		ic := github.IssueCommentEvent{
			Action: github.IssueCommentActionCreated,
			Repo: github.Repo{
				Owner: github.User{
					Login: "foo",
				},
				Name:     "bar",
				FullName: "foo/bar",
			},
			Issue: github.Issue{
				Number:      iNumber,
				State:       "closed",
				PullRequest: &struct{}{},
			},
			Comment: github.IssueComment{
				User: user,
				Body: "/cherrypick stage",
			},
		}

		botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}
		getSecret := func() []byte {
			return []byte("sha=abcdefg")
		}

		s := &Server{
			botUser:        botUser,
			gc:             c,
			push:           func(_, _ string, _ bool) error { return nil },
			ghc:            ghc,
			tokenGenerator: getSecret,
			log:            logrus.StandardLogger().WithField("client", "cherrypicker"),
			repos:          []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},

			prowAssignments: prowAssignments,
		}

		if err := s.handleIssueComment(logrus.NewEntry(logrus.StandardLogger()), ic); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var expected []github.User
		if prowAssignments {
			expected = append(expected, user)
		}

		got := ghc.prs[1].Assignees
		if !cmp.Equal(got, expected) {
			t.Errorf("Expected (%d):\n+%v\nGot (%d):\n%+v\n", len(expected), expected, len(got), got)
		}
	}
}

func TestHandleLocks(t *testing.T) {
	t.Parallel()
	s := &Server{
		ghc:     &threadUnsafeFGHC{fghc: &fghc{}},
		botUser: &github.UserData{},
	}

	routine1Done := make(chan struct{})
	routine2Done := make(chan struct{})

	l := logrus.WithField("test", t.Name())

	go func() {
		defer close(routine1Done)
		if err := s.handle(l, "", "", &github.IssueComment{}, "org", "repo", "targetBranch", "baseBranch", []string{}, "title", "body", 0, []github.Label{}); err != nil {
			t.Errorf("routine failed: %v", err)
		}
	}()
	go func() {
		defer close(routine2Done)
		if err := s.handle(l, "", "", &github.IssueComment{}, "org", "repo", "targetBranch", "baseBranch", []string{}, "title", "body", 0, []github.Label{}); err != nil {
			t.Errorf("routine failed: %v", err)
		}
	}()

	<-routine1Done
	<-routine2Done

	if actual := s.ghc.(*threadUnsafeFGHC).orgRepoCountCalled; actual != 2 {
		t.Errorf("expected two EnsureFork calls, got %d", actual)
	}
}

func TestEnsureForkExists(t *testing.T) {
	botUser := &github.UserData{Login: "ci-robot", Email: "ci-robot@users.noreply.github.com"}

	ghc := &fghc{}

	s := &Server{
		botUser: botUser,
		ghc:     ghc,
		repos:   []github.Repo{{Fork: true, FullName: "ci-robot/bar"}},
	}

	testCases := []struct {
		name     string
		org      string
		repo     string
		expected string
		errors   bool
	}{
		{
			name:     "Repo name does not change after ensured",
			org:      "whatever",
			repo:     "repo",
			expected: "repo",
			errors:   false,
		},
		{
			name:     "EnsureFork changes repo name",
			org:      "whatever",
			repo:     "changeme",
			expected: "changed",
			errors:   false,
		},
		{
			name:     "EnsureFork errors",
			org:      "whatever",
			repo:     "error",
			expected: "error",
			errors:   true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.ensureForkExists(tc.org, tc.repo)
			if tc.errors && err == nil {
				t.Errorf("expected error, but did not get one")
			}
			if !tc.errors && err != nil {
				t.Errorf("expected no error, but got one")
			}
			if res != tc.expected {
				t.Errorf("expected %s but got %s", tc.expected, res)
			}
		})
	}

}

func TestReleaseNoteFromParentPR(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "test1",
			input:    "```feature developer\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foo/bar #123 @foo-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test2",
			input:    "```feature developer github.com/foobar/barfoo #999 @bar-author\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foobar/barfoo #999 @bar-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test3",
			input:    "```feature developer github.com/foobar/barfoo #999\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foobar/barfoo #999 @foo-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test4",
			input:    "```feature developer github.com/foobar/barfoo\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foo/bar #123 @foo-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test5",
			input:    "```feature developer @bar-author\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foo/bar #123 @bar-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test6",
			input:    "```feature developer #999 @bar-author\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foo/bar #123 @bar-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test7",
			input:    "```feature developer github.com/foobar/barfoo @bar-author\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foo/bar #123 @bar-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test8",
			input:    "```feature developer #999\nUpdate the magic number from 42 to 49\n```",
			expected: "```feature developer github.com/foo/bar #123 @foo-author\nUpdate the magic number from 42 to 49\n```",
		},
		{
			name:     "test9",
			input:    "```feature developer\nUpdate the magic number from 42 to 49\n```\n```feature operator\nUpdate another magic number\n```",
			expected: "```feature developer github.com/foo/bar #123 @foo-author\nUpdate the magic number from 42 to 49\n```\n```feature operator github.com/foo/bar #123 @foo-author\nUpdate another magic number\n```",
		},
		{
			name:     "test10",
			input:    "foobar",
			expected: "",
		},
		{
			name:     "test11",
			input:    "```feature developer\nUpdate the magic number from 42 to 49\n```\n```feature operator\nUpdate another magic number\nThis time with multiple lines\n```",
			expected: "```feature developer github.com/foo/bar #123 @foo-author\nUpdate the magic number from 42 to 49\n```\n```feature operator github.com/foo/bar #123 @foo-author\nUpdate another magic number\nThis time with multiple lines\n```",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := releaseNoteFromParentPR("foo-author", "foo", "bar", 123, tc.input)
			if result != tc.expected {
				t.Errorf("Expected: %q\n Got: %q", tc.expected, result)
			}
		})
	}
}

type threadUnsafeFGHC struct {
	*fghc
	orgRepoCountCalled int
}

func (tuf *threadUnsafeFGHC) EnsureFork(_, _, _ string) (string, error) {
	tuf.orgRepoCountCalled++
	return "", errors.New("that is enough")
}

type prNumberGenerator struct {
	sync.Mutex
	prNumber int
}

func (p *prNumberGenerator) GetPRNumber() int {
	p.Lock()
	defer p.Unlock()
	p.prNumber = p.prNumber + 10
	return p.prNumber
}

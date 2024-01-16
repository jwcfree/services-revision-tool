package gitlab_helper

import (
	"os/exec"
    "path/filepath"
	"errors"
	"crypto/tls"
	"log"
	"net/http"
	"sources/logger"
	"strings"

	"github.com/xanzy/go-gitlab"
)

func GetProjectTag(gitClient *gitlab.Client, projectID int) []*gitlab.Tag {
	orderBy := "updated"
	sortBy := "desc"
	logger.Log.Tracef("Get project id: %d tags", projectID)
	tags, _, err := gitClient.Tags.ListTags(projectID, &gitlab.ListTagsOptions{
		OrderBy: &orderBy,
		Sort:    &sortBy,
	})
	if err != nil {
		logger.Log.Errorf("Could not get project %d tags: %v", projectID, err)
	}
	return tags
}

func GetGitlabClient(gitlabToken string, gitlabBaseURL string, skipTLS bool) *gitlab.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLS},
	}
	logger.Log.Tracef("Getting gitlab client")
	client := &http.Client{Transport: tr}
	optClient := gitlab.WithHTTPClient(client)
	gitClient, err := gitlab.NewClient(gitlabToken, gitlab.WithBaseURL(gitlabBaseURL), optClient)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	return gitClient
}

func GetProjectsInGroup(gitClient *gitlab.Client, gitlabGroupID string) []*gitlab.Project {
	var projects []*gitlab.Project
	opt := &gitlab.ListGroupProjectsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 50,
			Page:    1,
		},
	}
	logger.Log.Tracef("List projects in gitlab group %s", gitlabGroupID)
	for {
		projectsPart, resp, err := gitClient.Groups.ListGroupProjects(gitlabGroupID, opt)
		if err != nil {
			log.Fatalf("Error getting projects, check your setting or gitlab token: %v", err)
		}
		projects = append(projects, projectsPart...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return projects
}

func GetProjectsMap(projects []*gitlab.Project) map[string]*gitlab.Project {
	var tempProjectMap map[string]*gitlab.Project
	tempProjectMap = make(map[string]*gitlab.Project)
	for _, project := range projects {
		tempProjectMap[strings.TrimSpace(project.Path)] = project
	}
	if tempProjectMap == nil {
		log.Fatalf("Empty projects map, terminating")
	}
	return tempProjectMap
}

func GetProjectArchive(gitClient *gitlab.Client, gitlabProjectID int, format *string, sha *string) []byte {
	opt := &gitlab.ArchiveOptions{
		Format: format,
		SHA:    sha,
	}
	logger.Log.Tracef("Get project archive, id: %d, format: %v, sha: %v", gitlabProjectID, *format, *sha)
	tempBody, _, err := gitClient.Repositories.Archive(gitlabProjectID, opt, nil)
	if err != nil {
		log.Fatal(err)
	}
	return tempBody

}

func GetProjectID(gitClient *gitlab.Client, serviceName string) (int, error) {
	projects := GetProjectsInGroup(gitClient, "2706")
	projectsMap := GetProjectsMap(projects)
	project, ok := projectsMap[strings.TrimSpace(serviceName)]
	if !ok {
		return 0, errors.New("Service not found in the GitLab group")
	}
	return project.ID, nil
}

func CloneRepo(repoURL, branch, cloneDir string) (string, error) {
    cmd := exec.Command("git", "clone", "-b", branch, repoURL, cloneDir)
    if err := cmd.Run(); err != nil {
        return "", err
    }
    return filepath.Join(cloneDir, filepath.Base(repoURL)), nil
}
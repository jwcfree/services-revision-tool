// Пакет предназначен для сборки исходных кодов сервисов
package main

import (
	_ "embed"
	"fmt"
	"os"
	"sources/config"
	gitlab_helper "sources/gitlab"
	"sources/logger"
	"sources/services"
	"sync"
	"syscall"

	"github.com/xanzy/go-gitlab"
)

var (
	skipTLS bool
	//go:embed VERSION
	version string
)

// init Функция инициализации конфигурации и логера
func init() {
	config.Init()
	logger.InitLog()
}

func processSvc(idx int, svc string, projectsMap map[string]*gitlab.Project, cfg *config.Configuration, gitClient *gitlab.Client, wg *sync.WaitGroup, semaphore chan struct{}) {
	defer wg.Done()
	if projectsMap[svc] != nil {
		logger.Log.Debugf("service # %d: %s id: %d path: %v\n", idx, svc, projectsMap[svc].ID, projectsMap[svc].Path)
		logger.Log.Infof("Processing %s service", svc)
		apath := cfg.Output_dir + "/" + svc + "." + cfg.Archive_format
		err := os.WriteFile(apath, gitlab_helper.GetProjectArchive(gitClient, projectsMap[svc].ID, &cfg.Archive_format, &cfg.Branch), 0644)
		if err != nil {
			logger.Log.Fatalf("Error writing archive file: %v", err)
		}
		logger.Log.Debugf("Created archive for service # %d: %s id: %d\n", idx, svc, projectsMap[svc].ID)
		logger.Log.Debugf("Build service %s", svc)
		err2 := services.ProcessService(apath, projectsMap[svc])
		if err2 != nil {
			logger.Log.Errorf("Error process service %s", svc)
			os.Exit(1)
		}
		logger.Log.Debugf("Finish process service %s", svc)
		logger.Log.Debugf("Find dependencies for service %s", svc)

		if err != nil {
			logger.Log.Errorf("Could not build service %s, error: %v", svc, err)
		}
	} else {
		logger.Log.Warnf("Project %s not found\n", svc)
		services.UnknownProjects = append(services.UnknownProjects, svc)
	}

	logger.Log.Infof("Finished processing %s service. Details in Readme.md file", svc)
	<-semaphore
}

func main() {
	if config.Version {
		fmt.Printf("%s", version)
		os.Exit(0)
	}
	logger.Log.Info("Starting")
	var projects []*gitlab.Project
	var projectsRtlDeps []*gitlab.Project
	var projectsMap map[string]*gitlab.Project
	var projectsRtlDepsMap map[string]*gitlab.Project
	var gitClient *gitlab.Client
	var limit syscall.Rlimit
	skipTLS = true
	logger.Log.Debug("Set vars")
	cfg, err := config.ReadConfig(config.ConfigFile)
	if err != nil {
		logger.Log.Fatalln("Configuration could not be found")
	}
	err_config := config.CheckEmpty(*cfg)
	if err_config != nil {
		logger.Log.Fatalf("Terminating, error: %v", err_config)
	}
	services.Cfg = cfg
	gitlabToken := os.Getenv("GIT_TOKEN")
	if gitlabToken == "" {
		logger.Log.Fatalf("Please set gitlab token in GIT_TOKEN env var")
	}
	if config.ForceReplace {
		err = os.RemoveAll(cfg.Output_dir)
		if err != nil {
			logger.Log.Fatalf("Could not delete temp folder %s, error: %v", cfg.Output_dir, err)
		}
	}
	if config.ClearCache {
		if cfg.Cache == false {
			logger.Log.Fatalf("Could not clear cache dir, cache disabled in config file")
		}
		err = os.RemoveAll(cfg.CacheDir)
		if err != nil {
			logger.Log.Fatalf("Could not delete cache folder %s, error: %v", cfg.CacheDir, err)
		}

	}
	if cfg.Proxy {
		if cfg.ProxyHost == "" || cfg.ProxyUser == "" || cfg.ProxyPass == "" {
			logger.Log.Fatalf("Proxy urs/user/password could not be empty in config! Check your settings")
		}
	}

	logger.Log.Trace("Check system max open files limit value")
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		logger.Log.Fatalf("Unable to determine system parameters, terminating:" + err.Error())
	}
	logger.Log.Tracef("System ulimit size, soft: %d, hard: %d", limit.Cur, limit.Max)
	if (cfg.MaxParallelism * 2048) > int(limit.Max) {
		logger.Log.Fatalf("Unable to continue operation, current value max open files (%d) is too low. Please set it to > %d by command 'ulimit -n %d'",
			limit.Max, cfg.MaxParallelism*2048, cfg.MaxParallelism*2048)
	}
	if cfg.UploadToNexus {
		if os.Getenv("NEXUS_USER") == "" || os.Getenv("NEXUS_PASS") == "" {
			logger.Log.Fatalf("Please set nexus auth credentials in NEXUS_USER and NEXUS_PASS env vars or set `upload_to_nexus=false` in config.json")
		}
	}
	gitClient = gitlab_helper.GetGitlabClient(gitlabToken, cfg.Gitlab_api_host, skipTLS)
	services.GitClient = gitClient
	projects = gitlab_helper.GetProjectsInGroup(gitClient, cfg.Group_id)
	if projects == nil {
		logger.Log.Fatalf("Failed to retrieve any projects. Check if the group is correct")
	}
	projectsRtlDeps = gitlab_helper.GetProjectsInGroup(gitClient, cfg.RtlSearchRepoId)
	if projects == nil {
		logger.Log.Fatalf("Failed to retrieve any project for rtl.pgs dependencies. Check if the group is correct")
	}
	projectsMap = make(map[string]*gitlab.Project)
	projectsMap = gitlab_helper.GetProjectsMap(projects)
	projectsRtlDepsMap = make(map[string]*gitlab.Project)
	projectsRtlDepsMap = gitlab_helper.GetProjectsMap(projectsRtlDeps)
	services.ProjectsMap = projectsMap
	services.ProjectsRtlDepsMap = projectsRtlDepsMap
	if _, err := os.Stat(cfg.Output_dir); os.IsNotExist(err) {
		err := os.Mkdir(cfg.Output_dir, 0744)
		if err != nil {
			logger.Log.Fatalf("Error creating output dir: %v", err)
		}
	}
	if cfg.Cache {
		if _, err := os.Stat(cfg.CacheDir); os.IsNotExist(err) {
			logger.Log.Tracef("Create cache dir %s", cfg.CacheDir)
			err := os.Mkdir(cfg.CacheDir, 0744)
			if err != nil {
				logger.Log.Fatalf("Error creating cache dir: %v", err)
			}
		}
	}
	logger.Log.Info("Processing services")
	services.Init()
	var pmax = cfg.MaxParallelism
	semaphore := make(chan struct{}, pmax)
	wg := &sync.WaitGroup{}
	for idx, svc := range cfg.Service_list {
		if _, err := os.Stat(cfg.Output_dir + "/" + svc + "." + cfg.Archive_format); err == nil {
			logger.Log.Debugf("Service %s has already been processed, skipping", svc)
			continue

		}

		semaphore <- struct{}{}
		wg.Add(1)
		go processSvc(idx, svc, projectsMap, cfg, gitClient, wg, semaphore)

	}
	wg.Wait()
	services.SummaryReport("report.txt")

}

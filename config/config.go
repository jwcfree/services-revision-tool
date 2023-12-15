package config

import (
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"reflect"
)

var (
	LogLevel     string
	ConfigFile   string
	ForceReplace bool
	Version      bool
	ClearCache   bool
)

type Configuration struct {
	Gitlab_api_host       string   `json:"gitlab_api_host"`
	Output_dir            string   `json:"output_dir"`
	Service_list          []string `json:"service_list"`
	Group_id              string   `json:"group_id"`
	Branch                string   `json:"branch"`
	Archive_format        string   `json:"archive_format"`
	MavenUrl              string   `json:"maven_url"`
	PluginsUrl            string   `json:"plugins_url"`
	ReadmeTemplate        string   `json:"readme_template"`
	MaxParallelism        int      `json:"max_parallelism"`
	RtlSearchRepoId       string   `json:"rtl_search_repo_id"`
	UploadToNexus         bool     `json:"upload_to_nexus"`
	NexusUrl              string   `json:"nexus_url"`
	NexusPath             string   `json:"nexus_path"`
	NexusMavenUrl         string   `json:"nexus_maven_url"`
	NexusForceAddToGradle bool     `json:"nexus_force_add_to_gradle"`
	NexusAutoAddToGradle  bool     `json:"nexus_auto_add_to_gradle"`
	CacheDir              string   `json:"cache_dir"`
	Cache                 bool     `json:"cache"`
	Proxy                 bool     `json:"proxy"`
	ProxyHost             string   `json:"proxy_host"`
	ProxyPort             string   `json:"proxy_port"`
	ProxyUser             string   `json:"proxy_user"`
	ProxyPass             string   `json:"proxy_pass"`
}

func Init() {
	flag.StringVar(&LogLevel, "loglevel", "INFO", "Log level, could be WARN, DEBUG, TRACE, ERROR")
	flag.StringVar(&ConfigFile, "configfile", "config.json", "Path to json config file")
	flag.BoolVar(&ForceReplace, "force", false, "Force replace temp work files")
	flag.BoolVar(&Version, "v", false, "Show version")
	flag.BoolVar(&ClearCache, "c", false, "Clear cache dir")
	flag.Parse()
}

func ReadConfig(file string) (*Configuration, error) {
	cBytes, err := ioutil.ReadFile(file)
	if err != nil {
		log.Fatalf("Cannot read configuration file: %v", err.Error())
	}
	c := &Configuration{}
	err = json.Unmarshal(cBytes, &c)
	if err != nil {
		log.Fatalf("Cannot parse configuration file: %v", err.Error())
	}
	return c, nil
}

func CheckEmpty(c Configuration) error {
	v := reflect.ValueOf(c)
	typeOfS := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Interface() == "" {
			return errors.New("cannot parse configuration, empty field: " + typeOfS.Field(i).Name)
		}
	}
	return nil

}

package nexus

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"os"
	"sources/config"
	"sources/logger"
	"strconv"
	"strings"
)

var (
	Cfg *config.Configuration
)

func AddNexusToBuildGradle(file string, version string) error {
	logger.Log.Debugf("Add local nexus to repositorines in file: %s", file)
	f, err := os.OpenFile(file, os.O_RDWR, 0644)
	if err != nil {
		logger.Log.Errorf("Unable to open %s, error: %v", file, err)
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lines := []string{}
	for scanner.Scan() {
		ln := scanner.Text()
		if strings.Contains(strings.ToLower(ln), "mavencentral()") {
			gradleMajorVersion, _ := strconv.Atoi(strings.Split(version, ".")[0])
			if err != nil {
				logger.Log.Errorf("Could not get gradle major ver : %v", err)
				return err
			}
			logger.Log.Tracef("Gradle Major version is %d", gradleMajorVersion)
			if gradleMajorVersion > 5 {
				logger.Log.Tracef("Found mavenCentral, add local nexus with insecure flag for gradle version %s", version)
				lines = append(lines, "maven { url '"+Cfg.NexusMavenUrl+"' \nallowInsecureProtocol true\n  }")
				continue
			} else {
				logger.Log.Tracef("Found mavenCentral, add local nexus after for gradle version %s", version)
				lines = append(lines, "maven { url '"+Cfg.NexusMavenUrl+"' }")
				continue
			}
		}
		lines = append(lines, ln)
	}
	content := strings.Join(lines, "\n")
	_, err = f.WriteAt([]byte(content), 0)
	if err != nil {
		logger.Log.Errorf("Error writing %s : %v", file, err)
		return err
	}
	return nil
}

func AddNexusToSettingsGradle(file string, version string) error {
	logger.Log.Debugf("Add local nexus to repositorines in file: %s", file)
	f, err := os.OpenFile(file, os.O_RDWR, 0644)
	if err != nil {
		logger.Log.Errorf("Unable to open %s, error: %v", file, err)
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	lines := []string{}
	add_status := false
	gradleMajorVersion, _ := strconv.Atoi(strings.Split(version, ".")[0])
	if err != nil {
		logger.Log.Errorf("Could not get gradle major ver : %v", err)
		return err
	}
	for scanner.Scan() {
		ln := scanner.Text()
		if strings.Contains(strings.ToLower(ln), "repositories") && !add_status {
			logger.Log.Tracef("Gradle Major version is %d, settings gradle", gradleMajorVersion)
			if gradleMajorVersion > 5 {
				logger.Log.Tracef("Append settings.gradle with insecure flag for gradle version %s in file %s", version, file)
				lines = append(lines, "repositories {\nmaven { url '"+Cfg.NexusMavenUrl+"' \nallowInsecureProtocol true\n}")
				add_status = true
				continue
			} else {
				logger.Log.Tracef("Append settings.gradle for gradle version %s in file %s", version, file)
				lines = append(lines, "repositories {\nmaven { url '"+Cfg.NexusMavenUrl+"'}")
				add_status = true
				continue
			}
		}

		lines = append(lines, ln)
	}
	if !add_status {
		f := make([]string, len(lines)+1)
		copy(f[1:], lines)
		logger.Log.Tracef("Create plugins section in settings.gradle for gradle version %s in file %s", version, file)
		if gradleMajorVersion > 5 {
			f[0] = "pluginManagement {\nrepositories {\nmaven { url '" + Cfg.NexusMavenUrl + "' \nallowInsecureProtocol true\n}\n}\n}"
		} else {
			f[0] = "pluginManagement {\nrepositories {\nmaven { url '" + Cfg.NexusMavenUrl + "'}\n}\n}"
		}
		lines = f
	}
	content := strings.Join(lines, "\n")
	_, err = f.WriteAt([]byte(content), 0)
	if err != nil {
		logger.Log.Errorf("Error writing %s : %v", file, err)
		return err
	}
	return nil
}

func UploadNexus(f string, d string) error {
	arch, err := os.Open(f)
	client := &http.Client{}
	if err != nil {
		logger.Log.Fatalf("Error opening archive file: %v", err)
	}
	defer arch.Close()
	//	if Cfg.Proxy {
	//		auth := proxy.Auth{
	//			User:     Cfg.ProxyUser,
	//			Password: Cfg.ProxyPass,
	//		}
	//		dialer, err := proxy.SOCKS5("tcp", Cfg.ProxyUrl, &auth, proxy.Direct)
	//		if err != nil {
	//			logger.Log.Fatalf("Can't connect to the proxy: %v", err)
	//		}
	//		tr := &http.Transport{Dial: dialer.Dial}
	//		client = &http.Client{
	//			Transport: tr,
	//		}
	//	} else {
	//		client = &http.Client{}
	//	}
	payload := io.MultiReader(arch)
	req, err := http.NewRequest("PUT", Cfg.NexusUrl+Cfg.NexusPath+"/"+d, payload)
	if err != nil {
		return err
	}
	req.SetBasicAuth(os.Getenv("NEXUS_USER"), os.Getenv("NEXUS_PASS"))
	req.Header.Set("Accept", "*/*")
	logger.Log.Tracef("Upload request: %v", req)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		logger.Log.Debugf("Error uploading to nexus: %v", resp)
		return errors.New("Error uploading to nexus, status != 201")
	}

	return nil
}

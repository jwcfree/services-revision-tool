// services Пакет для работы с сервисами
package services

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"sources/config"
	gitlab_helper "sources/gitlab"
	"sources/logger"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cavaliergopher/grab/v3"
	cp "github.com/otiai10/copy"
	"github.com/xanzy/go-gitlab"
	"golang.org/x/exp/slices"
	"golang.org/x/net/html"
	"golang.org/x/net/proxy"
	"golang.org/x/text/encoding/charmap"
)

var (
	Cfg                 *config.Configuration //Cfg переменная с полями основного конфигурационного файла утилиты
	ProjectsMap         map[string]*gitlab.Project
	ProjectsRtlDepsMap  map[string]*gitlab.Project
	GitClient           *gitlab.Client
	Unknown_sx_deps     map[string][]string
	Unknown_sx_deps_ver map[string][]string
	Unknown_deps        map[string][]string
	Known_deps          map[string][]string
	Without_src_deps    map[string][]string
	MapMutex            = sync.RWMutex{}
	UnknownProjects     []string
)

// ProjectXml Тип реализующий структуру maven зависимостей
type ProjectXml struct {
	GroupId    string `xml:"groupId"`
	ArtifactId string `xml:"artifactId"`
	Version    string `xml:"version"`
}

// Init функция инициализации, создает необходимые map
func Init() {
	Known_deps = make(map[string][]string)
	Unknown_deps = make(map[string][]string)
	Unknown_sx_deps = make(map[string][]string)
	Unknown_sx_deps_ver = make(map[string][]string)
	Without_src_deps = make(map[string][]string)
}

func hashFile(f string) error {
	logger.Log.Tracef("Start hash calc for file %s", f)
	if _, err := os.Stat(f); errors.Is(err, os.ErrNotExist) {
		return err
	}
	cmd := exec.Command("bash", "-c", "cpverify -mk \""+f+"\"")
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return err
	}
	h_file, err := os.Create(f + ".gost")
	if err != nil {
		return err
	}
	defer h_file.Close()
	_, err = h_file.WriteString(outb.String())
	if err != nil {
		return err
	}
	logger.Log.Tracef("Hash for file %s: %s", f, outb.String())
	return nil
}

func ExtractTgz(gzipStream io.Reader, output string) error {
	uStream, err := gzip.NewReader(gzipStream)
	if err != nil {
		logger.Log.Errorf("Unable to gzip service")
	}

	tarRdr := tar.NewReader(uStream)
	logger.Log.Debugf("Check output dir %s", output)
	if _, err := os.Stat(output); os.IsNotExist(err) {
		err := os.Mkdir(output, 0744)
		if err != nil {
			logger.Log.Errorf("Error creating output dir: %v", err)
			return err
		}
	}
	for true {
		header, err := tarRdr.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("Error extracting gz: %s", err.Error())
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(output+"/"+header.Name, 0755); err != nil {
				return fmt.Errorf("Error extracting, failed to create output dir: %s", err.Error())
			}
		case tar.TypeReg:
			outFile, err := os.Create(output + "/" + header.Name)
			if err != nil {
				return fmt.Errorf("Error extracting, failed create output file failed: %s", err.Error())
			}
			if _, err := io.Copy(outFile, tarRdr); err != nil {
				return fmt.Errorf("Error extracting, failed copy contents: %s", err.Error())
			}
			outFile.Close()

		}
	}
	return nil
}

func CreateTgz(src string, buf io.Writer) error {
	zr := gzip.NewWriter(buf)
	tw := tar.NewWriter(zr)
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := fi.Mode()
	if mode.IsRegular() {
		header, err := tar.FileInfoHeader(fi, src)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		data, err := os.Open(src)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, data); err != nil {
			return err
		}
	} else if mode.IsDir() {
		filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
			header, err := tar.FileInfoHeader(fi, file)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(file)
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if !fi.IsDir() {
				data, err := os.Open(file)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}
			return nil
		})
	} else {
		return fmt.Errorf("error: file type not supported")
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := zr.Close(); err != nil {
		return err
	}
	return nil
}

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

// ParseDockerfile Фунция парсит докерфал и возвращает имя образа, команду для сборки и версию gradle
func ParseDockerfile(file string) (image string, command string, version string, err error) {
	logger.Log.Debugf("Start parsing Dockerfile %s", file)
	f, err := os.Open(file)
	if err != nil {
		logger.Log.Errorf("Unable to open Dockerfile.pgs, error: %v", err)
		return image, command, version, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	line := 1
	for scanner.Scan() {
		if strings.Contains(strings.ToLower(scanner.Text()), "as build") || strings.Contains(strings.ToLower(scanner.Text()), "as gradle_build") {
			image = strings.Split(strings.TrimSpace(scanner.Text()), " ")[1]
			if image == "" {
				return image, command, version, errors.New("Failed to determine image to build")
			}
			logger.Log.Debugf("Build image: %s", image)
			version = strings.Split(strings.Split(strings.TrimSpace(image), ":")[2], "-")[0]
			logger.Log.Debugf("Gradle version: %s", version)
		}
		if strings.Contains(strings.ToLower(scanner.Text()), " gradle ") {
			lens := len(scanner.Text())
			lasts := strings.LastIndex(scanner.Text(), "gradle")
			command = scanner.Text()[lasts:lens]
			logger.Log.Debugf("Parsed command: %s", command)
		}
		line++

		if err := scanner.Err(); err != nil {
			logger.Log.Errorf("Error parse Dockerfile: %v", err)
			return image, command, version, err
		}

	}
	if image == "" {
		return image, command, version, errors.New("Failed to determine image to build")
	}
	if command == "" {
		return image, command, version, errors.New("Failed to determine command to build")
	}
	if version == "" {
		return image, command, version, errors.New("Failed to determine gradle version")
	}
	if Cfg.Proxy {
		if Cfg.ProxyUser != "nil" {
			command = strings.Replace(command, "gradle ", " gradle -DsocksProxyHost="+Cfg.ProxyHost+" -DsocksProxyPort="+Cfg.ProxyPort+" -Djava.net.socks.username="+Cfg.ProxyUser+" -Djava.net.socks.password="+Cfg.ProxyPass+" ", 1)
		} else {
			command = strings.Replace(command, "gradle ", " gradle -DsocksProxyHost="+Cfg.ProxyHost+" -DsocksProxyPort="+Cfg.ProxyPort+" ", 1)
		}
	}

	return image, command, version, nil
}

func findDockerfile(rootdir string) string {
	var dir string
	var files []string
	err := filepath.Walk(rootdir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Log.Errorf("Error finding Dockerfile dir: %v", err)
			return nil
		}

		if !info.IsDir() && filepath.Base(path) == "Dockerfile.pgs2" {
			logger.Log.Debugf("Found Dockerfile.pgs2 file, path: %s dir: %s", path, filepath.Dir(path))
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		logger.Log.Errorf("Error finding Dockerfile dir: %v", err)
		return ""
	}
	if len(files) > 1 {
		logger.Log.Warnf("Found more than 1 Dockerfile.pgs2 file, will user first found")
	}
	dir = files[0]
	return dir
}

func SetCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	if strings.ToLower(charset) == "iso-8859-1" {
		return charmap.Windows1252.NewDecoder().Reader(input), nil
	}
	return nil, fmt.Errorf("Unknown charset: %s", charset)
}

func GetDepInfoFromPom(pompath string) ProjectXml {
	var (
		pxml ProjectXml
	)
	logger.Log.Tracef("Trying to parse dep: %v", pompath)
	xmlFile, err := os.Open(pompath)
	if err != nil {
		logger.Log.Errorf("Error opening *.pom file: %v", err)
	}
	defer xmlFile.Close()
	decoder := xml.NewDecoder(xmlFile)
	decoder.CharsetReader = SetCharsetReader
	err = decoder.Decode(&pxml)
	if err != nil {
		logger.Log.Errorf("Error parsing pom xml %s: %v\n", pompath, err)
		return pxml
	}
	return pxml
}

func GetDependencies(depspath string) []ProjectXml {
	var deps []ProjectXml
	logger.Log.Tracef("Looking deps in dir: %s", depspath)
	err := filepath.Walk(depspath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Log.Errorf("Error building dependencies tree: %v", err)
			return nil
		}
		m, err := filepath.Match("*.pom", filepath.Base(path))
		logger.Log.Tracef("Lookup in path: %s", filepath.Base(path))
		if err != nil {
			logger.Log.Errorf("Error building dependencies tree: %v", err)
			return nil
		}
		if !info.IsDir() && m {
			logger.Log.Tracef("Found pom.xml file, path: %s dir: %s", path, filepath.Dir(path))
			a := GetDepInfoFromPom(path)
			if a.Version == "" || a.GroupId == "" || strings.Contains(a.GroupId, "$") || strings.Contains(a.Version, "$") {
				parts := strings.Split(path, string(os.PathSeparator))
				a.GroupId = parts[len(parts)-5]
				a.ArtifactId = parts[len(parts)-4]
				a.Version = parts[len(parts)-3]
				logger.Log.Tracef("Error, no version in pom file, get from dir %v", a)

			}
			logger.Log.Tracef("Parsed dep: %v", a)
			deps = append(deps, a)
		}

		return nil
	})
	if err != nil {
		logger.Log.Errorf("Error finding dependencies: %v", err)
		return nil
	}
	return deps
}

func ParseLinks(body io.Reader) []string {
	var links []string
	logger.Log.Tracef("Start link parsing")
	z := html.NewTokenizer(body)
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return links
		case html.StartTagToken, html.EndTagToken:
			token := z.Token()
			if "a" == token.Data {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						links = append(links, attr.Val)
					}

				}
			}

		}
	}
}

func CheckInCache(p string) (haveSource bool, existInCache bool, err error) {
	logger.Log.Tracef("Check files %s in cache, path: %s", p, filepath.Join(Cfg.CacheDir, p))
	if _, err := os.Stat(filepath.Join("./", Cfg.CacheDir, p)); os.IsNotExist(err) {

		logger.Log.Tracef("Not found %s in cache dir %s", p, filepath.Join(Cfg.CacheDir, p))
		return false, false, nil
	}
	f, err := os.Open(filepath.Join("./", Cfg.CacheDir, p))
	if err != nil {
		return false, false, err
	}
	defer f.Close()
	d, err := f.Readdir(-1)
	j, s := false, false
	if err != io.EOF {
		logger.Log.Tracef("Cache exists for path %s", p)
		for _, file := range d {
			if strings.Contains(file.Name(), ".jar") && !strings.Contains(file.Name(), "javadoc.jar") && !strings.Contains(file.Name(), ".jar.") {
				j = true
			}
			if j && strings.Contains(file.Name(), "sources") {
				s = true
			}

			if strings.Contains(file.Name(), ".tgz") && !strings.Contains(file.Name(), ".tgz.") {
				s = true
			}

		}
		if s == true || j == false {
			return true, true, nil
		} else {
			return false, true, nil
		}
	}
	return false, false, nil
}

func SaveToCache(d string, f string, s string) error {
	logger.Log.Tracef("Saving to cache dir file: %s", s)
	if _, err := os.Stat(filepath.Join(Cfg.CacheDir, d)); os.IsNotExist(err) {
		err := os.MkdirAll(filepath.Join(Cfg.CacheDir, d), 0744)
		if err != nil {
			logger.Log.Fatalf("Error creating dep output dir %s: %v", filepath.Join(Cfg.CacheDir, d), err)
			return err
		}
	}
	src_file, err := os.Open(s)
	if err != nil {
		return err
	}
	defer src_file.Close()
	src_file_stat, err := src_file.Stat()
	if err != nil {
		return err
	}

	if !src_file_stat.Mode().IsRegular() {
		return errors.New("It not a regular file, terminating")
	}

	dst_file, err := os.Create(filepath.Join(Cfg.CacheDir, d, f))
	if err != nil {
		return err
	}
	defer dst_file.Close()
	io.Copy(dst_file, src_file)
	return nil
}

func DownloadDeps(deps []ProjectXml, saveto string, svcName string) error {
	logger.Log.Tracef("Downloading deps")
	var path string
	var dwg sync.WaitGroup
	client := &http.Client{}
	grab_client := grab.NewClient()
	if Cfg.Proxy {
		logger.Log.Debugf("Using socks proxy for http requests")
		auth := proxy.Auth{
			User:     Cfg.ProxyUser,
			Password: Cfg.ProxyPass,
		}
		dialer, err := proxy.SOCKS5("tcp", Cfg.ProxyHost+":"+Cfg.ProxyPort, &auth, proxy.Direct)
		if err != nil {
			logger.Log.Fatalf("Can't connect to the proxy: %v", err)
		}
		tr := &http.Transport{Dial: dialer.Dial}
		client = &http.Client{
			Transport: tr,
		}
		grab_client.HTTPClient = client
	}
	for i, d := range deps {
		MapMutex.Lock()
		Known_deps[svcName] = append(Known_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
		MapMutex.Unlock()
		if Cfg.Cache {
			src, c, err := CheckInCache(d.GroupId + "/" + d.ArtifactId + "/" + d.Version)
			if err != nil {
				logger.Log.Fatalf("Error reading cache files: %v", err)
				return err
			}
			if c {
				logger.Log.Tracef("Copy dependecy %s files from cache", d.GroupId+":"+d.ArtifactId+":"+d.Version)
				err := cp.Copy(filepath.Join(Cfg.CacheDir, d.GroupId, d.ArtifactId, d.Version), filepath.Join(saveto, d.GroupId, d.ArtifactId, d.Version))
				if err != nil {
					logger.Log.Fatalf("Error copying files from cache: %v", err)
				}
				if src == false {
					MapMutex.Lock()
					Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					MapMutex.Unlock()
				}
				continue
			}
		}
		a := [4]string{saveto, d.GroupId, d.ArtifactId, d.Version}
		path = ""
		for _, pathpart := range a {
			path = path + pathpart + "/"
			if _, err := os.Stat(path); os.IsNotExist(err) {
				err := os.Mkdir(path, 0744)
				if err != nil {
					logger.Log.Fatalf("Error creating dep output dir %s: %v", path, err)
					return err
				}
			}
		}
		dwg.Add(1)
		go func(i int, d ProjectXml) error {
			if d.GroupId == "sx.microservices" {
				logger.Log.Tracef("Systematica dependency, download from Gitlab : %v", d)
				if ProjectsMap[d.ArtifactId] != nil {
					logger.Log.Tracef("Found dependency in gitlab, project: %v", ProjectsMap[d.ArtifactId].Name)
					tags := gitlab_helper.GetProjectTag(GitClient, ProjectsMap[d.ArtifactId].ID)
					tagf := false
					for _, t := range tags {
						if t.Name == "v"+d.Version {
							tagf = true
						}
					}
					if tagf {
						ver := "v" + d.Version
						logger.Log.Tracef("Trying to download service %s:%s:%s from gitlab", d.GroupId, d.ArtifactId, d.Version)
						err := os.WriteFile(saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+d.ArtifactId+"."+Cfg.Archive_format,
							gitlab_helper.GetProjectArchive(GitClient, ProjectsMap[d.ArtifactId].ID, &Cfg.Archive_format, &ver), 0644)
						if err != nil {
							logger.Log.Fatalf("Error writing archive file: %v", err)
						}
						err = hashFile(saveto + "/" + d.GroupId + "/" + d.ArtifactId + "/" + d.Version + "/" + d.ArtifactId + "." + Cfg.Archive_format)
						if err != nil {
							logger.Log.Errorf("Error calc hash for file: %v", err)
						}
						if Cfg.Cache {
							err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, d.ArtifactId+"."+Cfg.Archive_format, saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+d.ArtifactId+"."+Cfg.Archive_format)
							if err != nil {
								logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
							}
							err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, d.ArtifactId+"."+Cfg.Archive_format+".gost", saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+d.ArtifactId+"."+Cfg.Archive_format+".gost")
							if err != nil {
								logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
							}
						}
					} else {
						logger.Log.Warnf("Not found version of dependency %s:%s:%s in gitlab", d.GroupId, d.ArtifactId, d.Version)
						MapMutex.Lock()
						Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
						Unknown_sx_deps_ver[svcName] = append(Unknown_sx_deps_ver[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
						MapMutex.Unlock()
					}

				} else {
					logger.Log.Warnf("Service %s:%s:%s not found in gitlab", d.GroupId, d.ArtifactId, d.Version)
					MapMutex.Lock()
					Unknown_sx_deps[svcName] = append(Unknown_sx_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					MapMutex.Unlock()
				}
				dwg.Done()
				return nil

			} else if d.GroupId == "rtl.pgs" {
				logger.Log.Tracef("Found rtl dependecy")
				if ProjectsRtlDepsMap[d.ArtifactId] != nil {
					logger.Log.Tracef("Found dependency in gitlab, project: %v", ProjectsRtlDepsMap[d.ArtifactId])
					tags := gitlab_helper.GetProjectTag(GitClient, ProjectsRtlDepsMap[d.ArtifactId].ID)
					tagf := false
					ver := ""
					for _, t := range tags {
						if t.Name == "v"+d.Version {
							tagf = true
							ver = "v" + d.Version
						} else if t.Name == d.Version {
							tagf = true
							ver = d.Version
						}
					}
					if tagf {
						logger.Log.Tracef("Trying to download service %s:%s:%s from gitlab", d.GroupId, d.ArtifactId, d.Version)
						err := os.WriteFile(saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+d.ArtifactId+"."+Cfg.Archive_format,
							gitlab_helper.GetProjectArchive(GitClient, ProjectsRtlDepsMap[d.ArtifactId].ID, &Cfg.Archive_format, &ver), 0644)
						if err != nil {
							logger.Log.Fatalf("Error writing archive file: %v", err)
						}
						err = hashFile(saveto + "/" + d.GroupId + "/" + d.ArtifactId + "/" + d.Version + "/" + d.ArtifactId + "." + Cfg.Archive_format)
						if err != nil {
							logger.Log.Errorf("Error calc hash for file: %v", err)
						}
						if Cfg.Cache {
							err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, d.ArtifactId+"."+Cfg.Archive_format, saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+d.ArtifactId+"."+Cfg.Archive_format)
							if err != nil {
								logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
							}
							err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, d.ArtifactId+"."+Cfg.Archive_format+".gost", saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+d.ArtifactId+"."+Cfg.Archive_format+".gost")
							if err != nil {
								logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
							}
						}
					} else {
						logger.Log.Warnf("Not found version of dependency %s:%s:%s in gitlab", d.GroupId, d.ArtifactId, d.Version)
						MapMutex.Lock()
						Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
						Unknown_sx_deps_ver[svcName] = append(Unknown_sx_deps_ver[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
						MapMutex.Unlock()
					}

				} else {
					logger.Log.Warnf("Service %s:%s:%s not found in gitlab", d.GroupId, d.ArtifactId, d.Version)
					MapMutex.Lock()
					Unknown_sx_deps[svcName] = append(Unknown_sx_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					MapMutex.Unlock()
				}
				dwg.Done()
				return nil
			}
			url := Cfg.MavenUrl + "/" + strings.Replace(d.GroupId, ".", "/", -1) + "/" + d.ArtifactId + "/" + d.Version
			logger.Log.Debugf("URL for deps download: %v", url)
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				logger.Log.Errorf("Could not create http request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				logger.Log.Errorf("Error get maven dependency index html, error %v", err)
				MapMutex.Lock()
				Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
				MapMutex.Unlock()
			}
			defer resp.Body.Close()
			var l []string
			if resp.StatusCode == 404 {
				logger.Log.Debugf("Error get maven dependency index html, dependency %v. Will try Plugins repo", d)
				url = Cfg.PluginsUrl + "/" + strings.Replace(d.GroupId, ".", "/", -1) + "/" + d.ArtifactId + "/" + d.Version
				req, err := http.NewRequest(http.MethodGet, url, nil)
				if err != nil {
					logger.Log.Errorf("Could not create http request: %v", err)
				}
				resp, err := client.Do(req)
				if resp.StatusCode == 404 || resp.StatusCode != 200 {
					logger.Log.Debugf("Error get maven dependency index html, unknown dependency %v url %v.", d, url)
					MapMutex.Lock()
					Unknown_deps[svcName] = append(Unknown_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					MapMutex.Unlock()
				} else {
					l = ParseLinks(resp.Body)
					if l == nil {
						logger.Log.Errorf("Error parsing maven dependency links on page %s", url)
						MapMutex.Lock()
						Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
						MapMutex.Unlock()
						os.Exit(1)
					}
					logger.Log.Tracef("Parsed links: %v", l)
				}
				if err != nil {
					logger.Log.Errorf("Error get maven dependency index html, error %v", err)
					MapMutex.Lock()
					Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					MapMutex.Unlock()
				}
				defer resp.Body.Close()

			} else if resp.StatusCode == 200 {
				l = ParseLinks(resp.Body)
				if l == nil {
					logger.Log.Errorf("Error parsing maven dependency links on page %s", url)
					MapMutex.Lock()
					Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
					MapMutex.Unlock()
					os.Exit(1)
				}
				logger.Log.Tracef("Parsed links: %v", l)
			} else {
				logger.Log.Errorf("Error get maven dependecy, http request failed, error %v", err)
				MapMutex.Lock()
				Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
				MapMutex.Unlock()
			}

			j := false
			s := false
			for _, v := range l {
				if v != "../" {
					if strings.Contains(v, ".jar") && !strings.Contains(v, "javadoc.jar") && !strings.Contains(v, ".jar.") {
						j = true
					}
					if j && strings.Contains(v, "sources") {
						s = true
					}

					logger.Log.Tracef("Downloading : %v url: %s", d, url+"/"+v)
					req, err := grab.NewRequest(saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version, url+"/"+v)
					if err != nil {
						logger.Log.Fatalf("Could not create new request: %v", err)
					}
					resp := grab_client.Do(req)
					err = resp.Err()
					//resp, err := grab.Get(saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version, url+"/"+v)
					if err != nil {
						r := 5 + rand.Intn(10)
						logger.Log.Debugf("Unable to download dependency file, wait %ds and retry. Service: %s, dependecy: %s", r, svcName, d.GroupId+":"+d.ArtifactId+":"+d.Version)
						time.Sleep(time.Duration(r) * time.Second)
						req, err := grab.NewRequest(saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version, url+"/"+v)
						if err != nil {
							logger.Log.Fatalf("Could not create new request: %v", err)
						}
						resp := grab_client.Do(req)
						err = resp.Err()
						//resp, err = grab.Get(saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version, url+"/"+v)
						if err != nil {
							logger.Log.Errorf("Unable to download dependency file: %s : %v", d, err)
							MapMutex.Lock()
							Unknown_sx_deps[svcName] = append(Unknown_sx_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
							Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
							MapMutex.Unlock()
							return err
						}
						if Cfg.Cache {
							err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, v, saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+v)
							if err != nil {
								logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
							}
						}
					}
					if strings.Contains(v, ".jar") || strings.Contains(v, ".pom") {
						if !strings.Contains(v, ".jar.") && !strings.Contains(v, ".pom.") {
							err := hashFile(saveto + "/" + d.GroupId + "/" + d.ArtifactId + "/" + d.Version + "/" + v)
							if err != nil {
								logger.Log.Errorf("Error calc hash for file: %v", err)
							}
							if Cfg.Cache {
								err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, v+".gost", saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+v+".gost")
								if err != nil {
									logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
								}
							}
						}
					}
					if Cfg.Cache {
						err = SaveToCache(d.GroupId+"/"+d.ArtifactId+"/"+d.Version, v, saveto+"/"+d.GroupId+"/"+d.ArtifactId+"/"+d.Version+"/"+v)
						if err != nil {
							logger.Log.Fatalf("Could not save %s:%s:%s file to cache dir: %v", d.GroupId, d.ArtifactId, d.Version, err)
						}
					}
					if resp.HTTPResponse.StatusCode == 404 {
						logger.Log.Errorf("Unable to download dependency %s, maybe broken link", url)

					}
					if resp.IsComplete() {
						logger.Log.Tracef("Downloaded dependency %s with responce code: %v, ContentLength: %d bytes", d, resp.HTTPResponse.StatusCode, resp.HTTPResponse.ContentLength)
					}
				}
			}
			if !s && j {
				logger.Log.Tracef("Sources not found for dependency %s", d.GroupId+":"+d.ArtifactId+":"+d.Version)
				MapMutex.Lock()
				Without_src_deps[svcName] = append(Without_src_deps[svcName], d.GroupId+":"+d.ArtifactId+":"+d.Version)
				MapMutex.Unlock()
			}
			dwg.Done()
			return err
		}(i, d)
	}
	dwg.Wait()
	return nil
}

// Функция для упрощения создания конечного архива файлов.
// На вход принимает путь вида /tmp/folder, на выходе создаст архив /tmp/folder.tgz и удалит папку.
func packFolder(path string) error {
	fdest_src, err := os.Create(path + "." + Cfg.Archive_format)
	if err != nil {
		return errors.New("Error creating final archive file")
	}
	defer fdest_src.Close()
	err = CreateTgz(path, fdest_src)
	if err != nil {
		return errors.New("Error creating dependencies sources archive file")
	}
	err = os.RemoveAll(path)
	if err != nil {
		return errors.New("Could not delete folder")
	}
	err = hashFile(path + "." + Cfg.Archive_format)
	if err != nil {
		return errors.New("Could not calc hash for file")
	}
	return nil

}

func ProcessService(svcArchive string, svc *gitlab.Project) error {
	if _, err := os.Stat(svcArchive); os.IsNotExist(err) {
		log.Fatalf("Unable to build service, archive not fount: %v", err)
	}
	svcName := strings.TrimSpace(svc.Name)
	logger.Log.Debugf("Start builder")
	logger.Log.Debugf("Trying to open archive %s", svcArchive)
	r, err := os.Open(svcArchive)
	if err != nil {
		logger.Log.Errorf("Error opening archive %s %v", svcArchive, err)
		return err
	}
	logger.Log.Debugf("Trying to extract archive %s", svcArchive)
	defer r.Close()
	err = ExtractTgz(r, Cfg.Output_dir+"/"+svcName)
	if err != nil {
		logger.Log.Errorf("Error extracting archive %s %v", svcArchive, err)
		return err
	}
	if _, err := os.Stat(Cfg.Output_dir + "/" + svcName + "/docker_images"); os.IsNotExist(err) {
		err := os.Mkdir(Cfg.Output_dir+"/"+svcName+"/docker_images", 0744)
		if err != nil {
			logger.Log.Fatalf("Error creating docker_images dir: %v", err)
		}
	}
	if _, err := os.Stat(Cfg.Output_dir + "/" + svcName + "/gradle_configs"); os.IsNotExist(err) {
		err := os.Mkdir(Cfg.Output_dir+"/"+svcName+"/gradle_configs", 0744)
		if err != nil {
			logger.Log.Fatalf("Error creating gradle_configs dir: %v", err)
		}
	}
	dockerfile := findDockerfile(Cfg.Output_dir + "/" + svcName)
	if _, err := os.Stat(path.Dir(dockerfile) + "/build.gradle"); os.IsNotExist(err) {
		logger.Log.Errorf("Unable to find build.gradle file, maybe not gradle service, skipping: %v", err)
		err = os.RemoveAll(Cfg.Output_dir + "/" + svcName)
		if err != nil {
			logger.Log.Fatalf("Could not delete temp folder %s, error: %v", Cfg.Output_dir+"/"+svcName, err)
		}
		err = os.RemoveAll(Cfg.Output_dir + "/" + svcName + "." + Cfg.Archive_format)
		if err != nil {
			logger.Log.Fatalf("Could not delete temp folder %s, error: %v", Cfg.Output_dir+"/"+svcName, err)
		}
		return err
	}
	image, command, version, err := ParseDockerfile(dockerfile)
	if err != nil {
		logger.Log.Errorf("Unable to build service: %s", err)
		return err
	}
	logger.Log.Tracef("Start parsing build.gradle")
	if Cfg.NexusForceAddToGradle == true {
		err = AddNexusToBuildGradle(path.Dir(dockerfile)+"/build.gradle", version)
		if err != nil {
			logger.Log.Errorf("Unable to parse build.gradle service: %s", err)
			return err
		}
		err = AddNexusToSettingsGradle(path.Dir(dockerfile)+"/settings.gradle", version)
		if err != nil {
			logger.Log.Errorf("Unable to parse settings.gradle service: %s", err)
			return err
		}
	}
	logger.Log.Debugf("Run gradle build")
	ex, err := os.Executable()
	if err != nil {
		logger.Log.Panicf("Unable to get current executable path, terminating. %v", err)
		return err
	}
	cmd := exec.Command("bash", "-c", "docker run --user 1000 -v \""+filepath.Dir(ex)+"/"+filepath.Dir(dockerfile)+"\":/home/gradle "+image+" bash -c \"export GRADLE_USER_HOME=gradle_cache && "+command+" -g gradle_cache -q \"")
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	logger.Log.Debugf("Build command %v", cmd)
	if err := cmd.Run(); err != nil {
		if Cfg.NexusAutoAddToGradle {
			logger.Log.Debugf("Error run docker build for: %s, try to add local nexus repo", svc.Name)
			err = AddNexusToBuildGradle(path.Dir(dockerfile)+"/build.gradle", version)
			if err != nil {
				logger.Log.Errorf("Unable to parse build.gradle service: %s", err)
				return err
			}
			if strings.Contains(strings.ToLower(errb.String()), "gradle core plugins") {
				err = AddNexusToSettingsGradle(path.Dir(dockerfile)+"/settings.gradle", version)
				if err != nil {
					logger.Log.Errorf("Unable to parse build.gradle service: %s", err)
					return err
				}
			}
			logger.Log.Tracef("Run docker build again for service: %s ", svc.Name)
			cmd2 := exec.Command("bash", "-c", "docker run --user 1000 -v \""+filepath.Dir(ex)+"/"+filepath.Dir(dockerfile)+"\":/home/gradle "+image+" bash -c \"export GRADLE_USER_HOME=gradle_cache && "+command+" -g gradle_cache -q \"")
			var outb2, errb2 bytes.Buffer
			cmd2.Stdout = &outb2
			cmd2.Stderr = &errb2
			if err := cmd2.Run(); err != nil {
				logger.Log.Errorf("Error run docker build, second run, %s: %v, Build stdout: %s, stderr: %s", svc.Name, err, outb2.String(), errb2.String())
				return errors.New("Build command retry unsuccessfull")
			} else {

				logger.Log.Tracef("Build stdout: %s, stderr: %s", outb2.String(), errb2.String())
			}
		} else {
			logger.Log.Errorf("Error run docker build %s: %v, Build stdout: %s, stderr: %s", svc.Name, err, outb.String(), errb.String())
			return errors.New("Build command unsuccessfull")
		}
	} else {
		logger.Log.Tracef("Build stdout: %s, stderr: %s", outb.String(), errb.String())
	}
	var outb3, errb3 bytes.Buffer
	rp := strings.NewReplacer(
		"-", "_",
		" ", "_",
		",", "_",
		":", "_",
		".", "_",
		"/", "_",
	)
	image_f := rp.Replace(image)
	cmd3 := exec.Command("bash", "-c", "docker image save "+image+" -o \""+Cfg.Output_dir+"/"+svcName+"/docker_images/"+image_f+".tar\"")
	cmd3.Stdout = &outb3
	cmd3.Stderr = &errb3
	logger.Log.Tracef("Save docker image %s to %s", image, Cfg.Output_dir+"/"+svcName+"/docker_images/"+image_f+".tar")
	if err := cmd3.Run(); err != nil {
		logger.Log.Errorf("Error saving docker image %s, stdout: %s, stderr: %s", image, outb3.String(), errb3.String())
		return errors.New("Build command unsuccessfull")
	}
	logger.Log.Debugf("Copy gradle configs for service %s", svcName)
	err = cp.Copy(filepath.Dir(dockerfile)+"/build.gradle", Cfg.Output_dir+"/"+svcName+"/gradle_configs/build.gradle")
	if err != nil {
		logger.Log.Fatalf("Error copying files gradle configs: %v", err)
	}
	err = cp.Copy(filepath.Dir(dockerfile)+"/settings.gradle", Cfg.Output_dir+"/"+svcName+"/gradle_configs/settings.gradle")
	if err != nil {
		logger.Log.Fatalf("Error copying files gradle configs: %v", err)
	}
	logger.Log.Debugf("Find dependencies for service %s", svcName)
	deps := GetDependencies(filepath.Dir(dockerfile) + "/gradle_cache/caches/modules-2/files-2.1/")
	if deps == nil {
		logger.Log.Errorf("Unable to find any dependencies for service %s", svcName)
	}
	for i, dep := range deps {
		logger.Log.Tracef("Found %d dep: %s", i, dep)
	}
	logger.Log.Debugf("Trying to download dependencies for service %s", svcName)
	err = DownloadDeps(deps, Cfg.Output_dir+"/"+svcName+"/deps_sources", svcName)
	if err != nil {
		logger.Log.Errorf("Error downloading dependencies : %v", err)
		return err
	}
	s, err := PrepareFinalDir(svc)
	if err != nil {
		logger.Log.Errorf("Error processing final directory : %v", err)
		return err
	}
	logger.Log.Tracef("Finali %v", s)
	err = CreateReadme(svcName, svc.Path, s, Cfg.ReadmeTemplate)
	if err != nil {
		logger.Log.Errorf("Error creating Readme.md file: %v", err)
	}
	// Создаем tgz для подпапок
	folders := []string{"deps_sources", "docker_images", "gradle_dependencies", "gradle_configs"}
	for _, v := range folders {
		err = packFolder(Cfg.Output_dir + "/" + svcName + "/" + v)
		if err != nil {
			logger.Log.Errorf("Error processing folder %s : %v", v, err)
		}
	}
	err = packFolder(Cfg.Output_dir + "/" + svcName)
	if err != nil {
		logger.Log.Errorf("Error processing folder %s : %v", Cfg.Output_dir+"/"+svcName, err)
	}
	if Cfg.UploadToNexus {
		logger.Log.Infof("Uploading to nexus %s", Cfg.Output_dir+"/"+svcName+"."+Cfg.Archive_format)
		err := uploadNexus(Cfg.Output_dir+"/"+svcName+"."+Cfg.Archive_format, svcName+"."+Cfg.Archive_format)
		if err != nil {
			logger.Log.Fatalf("Error uploading to Nexus: %v", err)
		}
		err = uploadNexus(Cfg.Output_dir+"/"+svcName+"."+Cfg.Archive_format+".gost", svcName+"."+Cfg.Archive_format+".gost")
		if err != nil {
			logger.Log.Fatalf("Error uploading to Nexus: %v", err)
		}
		err = os.RemoveAll(Cfg.Output_dir + "/" + svcName + "." + Cfg.Archive_format)
		if err != nil {
			logger.Log.Fatalf("Could not delete uploaded archive %s, error: %v", Cfg.Output_dir+"/"+svcName+"."+Cfg.Archive_format, err)
		}

	}
	return nil
}

func uploadNexus(f string, d string) error {
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

func PrepareFinalDir(svc *gitlab.Project) (string, error) {
	var p string
	svcName := strings.TrimSpace(svc.Name)
	svcPath := strings.TrimSpace(svc.Path)
	logger.Log.Debugf("Prepare final dir fir service: %s, path: %s", svcName, svcPath)
	if _, err := os.Stat(Cfg.Output_dir + "/" + svcName); os.IsNotExist(err) {
		logger.Log.Fatalf("Unable to find service dir %v", err)
	}
	dockerfile := findDockerfile(Cfg.Output_dir + "/" + svcName)
	errf := os.Rename(filepath.Dir(dockerfile)+"/gradle_cache", Cfg.Output_dir+"/"+svcName+"/gradle_dependencies")
	if errf != nil {
		logger.Log.Fatalf("Unable to move dependencies dir to destination archive %v", errf)
	}
	errf = os.RemoveAll(filepath.Dir(dockerfile))
	if errf != nil {
		logger.Log.Fatalf("Unable to delete dependencies dir %v", errf)
	}
	errf = os.Rename(Cfg.Output_dir+"/"+svcPath+"."+Cfg.Archive_format, Cfg.Output_dir+"/"+svcName+"/"+svcPath+"."+Cfg.Archive_format)
	if errf != nil {
		logger.Log.Fatalf("Unable to move sources archive to destination archive %v", errf)
	}
	p = Cfg.Output_dir + "/" + svcName
	return p, nil
}

func CreateReadme(svc string, svcPath string, path string, tmplFile string) error {
	type TemplateStrings struct {
		SvcName         string
		Branch          string
		Url             string
		Deps            []string
		Deps_no_sources []string
		Deps_no_ver     []string
		Deps_unknown    []string
		Deps_sx_unknown []string
	}
	logger.Log.Debugf("Processing Readme.md file for service %s", svc)
	sort.Strings(Known_deps[svc])
	sort.Strings(Without_src_deps[svc])
	sort.Strings(Unknown_sx_deps_ver[svc])
	sort.Strings(Unknown_deps[svc])
	sort.Strings(Unknown_sx_deps[svc])
	logger.Log.Debugf("Processing 2 Readme.md file for service %s", svc)

	t := TemplateStrings{svc, Cfg.Branch, ProjectsMap[svcPath].HTTPURLToRepo, slices.Compact(Known_deps[svc]),
		slices.Compact(Without_src_deps[svc]),
		slices.Compact(Unknown_sx_deps_ver[svc]),
		slices.Compact(Unknown_deps[svc]),
		slices.Compact(Unknown_sx_deps[svc])}
	logger.Log.Debugf("Processing 3 Readme.md file for service %s", svc)
	if _, err := os.Stat(tmplFile); os.IsNotExist(err) {
		logger.Log.Fatalf("Unable to find template, error: %v", err)
	}
	tmpl, err := template.ParseFiles(tmplFile)
	logger.Log.Tracef("Parsed tmpl file %s, %v", tmplFile, tmpl)
	if err != nil {
		logger.Log.Fatalf("Template parsing error: %v ", err)
	}
	w, err := os.Create(path + "/README.md")
	logger.Log.Tracef("Created README.md file %s", path+"/README.md")
	if err != nil {
		logger.Log.Fatalf("Error create template filer: %v ", err)
	}
	defer w.Close()
	logger.Log.Tracef("Execute template engine")
	err = tmpl.Execute(w, t)
	if err != nil {
		logger.Log.Fatalf("Template output error: %v ", err)
	}
	if err != nil {
		logger.Log.Fatalf("Error writing archive file: %v", err)
	}
	return nil
}

func SummaryReport(r string) error {
	var ds_unkown map[string][]string
	var ds_known []string
	var dsws_unkown map[string][]string
	ds_unkown = make(map[string][]string)
	dsws_unkown = make(map[string][]string)
	logger.Log.Debugf("Processing summary report file: %s", r)
	w, err := os.Create(Cfg.Output_dir + "/" + r)
	if err != nil {
		logger.Log.Fatalf("Error create summary report file: %v", err)
	}
	defer w.Close()
	_, err = w.WriteString("Обработанные сервисы:\n\n")
	if err != nil {
		logger.Log.Fatalf("Error write to report file: %v", err)
	}
	for svc, _ := range Known_deps {
		_, err = w.WriteString(svc + "\n")
		if err != nil {
			logger.Log.Fatalf("Error write to report file: %v", err)
		}
	}

	_, err = w.WriteString("\nНе найденные проекты:\n\n")
	if err != nil {
		logger.Log.Fatalf("Error write to report file: %v", err)
	}
	for _, svc := range UnknownProjects {
		_, err = w.WriteString(svc + "\n")
		if err != nil {
			logger.Log.Fatalf("Error write to report file: %v", err)
		}
	}

	_, err = w.WriteString("\nНе найденные зависимости (формат сервис/библиотека):\n\n")
	if err != nil {
		logger.Log.Fatalf("Error write to report file: %v", err)
	}
	for svc, dep := range Unknown_deps {
		for _, d := range dep {
			ds_unkown[d] = append(ds_unkown[d], svc)
		}
	}
	for svc, dep := range Without_src_deps {
		for _, d := range dep {
			dsws_unkown[d] = append(dsws_unkown[d], svc)
		}
	}
	for dep, svc := range ds_unkown {
		logger.Log.Tracef("Unknown dependecy: %s", dep)
		for _, s := range svc {
			logger.Log.Tracef("Unkonwn dependecy service: %s", s)
			_, err = w.WriteString(s + "/" + dep + "\n")
			if err != nil {
				logger.Log.Fatalf("Error write to report file: %v", err)
			}
		}
	}
	_, err = w.WriteString("\nЗависимости без исходных кодов (формат сервис/библиотека):\n\n")
	for dep, svc := range dsws_unkown {
		for _, s := range svc {
			_, err = w.WriteString(s + "/" + dep + "\n")
			if err != nil {
				logger.Log.Fatalf("Error write to report file: %v", err)
			}
		}
	}
	_, err = w.WriteString("\nСписок зависимостей (Список зависимостей по каждому сервису):\n\n")
	if err != nil {
		logger.Log.Fatalf("Error write to report file: %v", err)
	}
	for svc, svc_dep := range Known_deps {
		_, err = w.WriteString("\n----------------\nСервис: " + svc + "\n")
		if err != nil {
			logger.Log.Fatalf("Error write to report file: %v", err)
		}
		for _, s := range svc_dep {
			_, err = w.WriteString(s + "\n")
			if err != nil {
				logger.Log.Fatalf("Error write to report file: %v", err)
			}
			ds_known = append(ds_known, s)
		}
	}
	total_ds := slices.Compact(ds_known)
	_, err = w.WriteString("\nОбщий Список зависимостей (Список зависимостей по всем сервисам сразу):\n\n")
	if err != nil {
		logger.Log.Fatalf("Error write to report file: %v", err)
	}
	for _, s := range total_ds {
		_, err = w.WriteString(s + "\n")
		if err != nil {
			logger.Log.Fatalf("Error write to report file: %v", err)
		}
	}
	logger.Log.Tracef("Finished processing summary report file: %s", r)

	return nil
}

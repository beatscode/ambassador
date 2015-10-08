package ambassador

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"testing"

	"github.com/samalba/dockerclient"
)

var bitbucketObject BitbucketPayload

func init() {
	byteArray, err := ioutil.ReadFile("bitbucket.push.json")
	if err != nil {
		panic(err)
	}

	json.Unmarshal(byteArray, &bitbucketObject)
	ConfDirectory = "./testConf"

	ApplicationDataPath = "./applicationDataFiles"

	docker, _ = dockerclient.NewDockerClient("unix:///var/run/docker.sock", nil)
}
func TestBitbucketPushLookup(t *testing.T) {

	fmt.Println(bitbucketObject.Repository.Name)

	if bitbucketObject.Repository.Name == "leonardo" {
		t.Log("Repository Name Passed")
	}

	if len(bitbucketObject.Push.Changes) > 0 {
		t.Log("Found Changes Slice")
	} else {
		t.Error("Error finding Push CHanges")
	}
	if bitbucketObject.GetBranchName() == "master" {
		t.Log("Found Branch Name successfully")
	} else {
		t.Error("Could not find correct branch name")
	}
}

func TestReplaceStringInFile(t *testing.T) {
	var filePath = "./tmpFile.txt"
	f, e := os.Create(filePath)
	if e != nil {
		t.Error(e)
	}
	defer f.Close()
	f.WriteString("git clone -b hhvm")

	replacer := strings.NewReplacer("git clone -b hhvm", fmt.Sprintf("%s", "git clone -b newbranch"))
	s := ReplaceStringInFile(filePath, replacer)

	if s == "git clone -b newbranch" {
		t.Log("Success")
	} else {
		t.Error("Failed to replace string in file")
	}

	f.Truncate(0)
	f.WriteString("git clone -b hhvm")
}

func TestListContainers(t *testing.T) {
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	if len(containers) > 0 {
		t.Log("Success")
	} else {
		t.Error("Could not get any containers")
	}
}
func TestExec(t *testing.T) {
	//var sContainer dockerclient.Container
	var config dockerclient.ExecConfig
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/testapi" {
				config.Container = c.Id
			}
		}
	}
	if config.Container == "" {
		t.Error("Container Not found")
	}
	config.Cmd = []string{"bash", "-c", "date"}
	config.AttachStdout = true
	config.AttachStderr = true
	config.AttachStdin = false
	config.Tty = false
	config.Detach = false
	ID, err := docker.ExecCreate(&config)
	fmt.Println(ID)
	if err != nil {
		t.Error(err)
	}

	config.Cmd = []string{}
	config.AttachStdout = true
	config.AttachStderr = true
	config.AttachStdin = true
	config.Tty = true
	config.Detach = true
	//fmt.Print(config)
	err = docker.ExecStart(ID, &config)
	if err != nil {
		t.Error(err)
	}

	//fmt.Print(containers)
}
func TestRewriteConf(t *testing.T) {
	var sApplicationData ApplicationData
	manifests := loadApplicationDataFiles()

	bitbucketObject.Repository.Name = "test"
	var foundApp bool
	for _, manifest := range manifests {
		if strings.ToLower(manifest.Name) == strings.ToLower(bitbucketObject.GetRepositoryName()) {
			foundApp = true
			sApplicationData = manifest
		}
	}

	sApplicationData.IP = "1.2.3.4"
	sApplicationData.Port = "52335"

	if !foundApp {
		t.Error("Could not find application manifest")
	}

	UpdateApplicationNginxConf(sApplicationData)

	b, err := ioutil.ReadFile("testConf/test.conf")
	if err != nil {
		panic(err)
	}

	if strings.Contains(string(b), "1.2.3.4:52335") == false {
		t.Error("Could not parse IP And port properly")
	} else {
		t.Log("Success")
	}

	if strings.Contains(string(b), "server_name a.b.com;") {
		t.Log("Success")
	} else {
		t.Error("Could not parse servername")
	}

	if strings.Contains(string(b), "root /var/www/testroot/public") {
		t.Log("Success")
	} else {
		t.Error("Could not parse root directory")
	}
}

func TestReadApplicationData(t *testing.T) {
	ApplicationDataPath = "./applicationDataFiles"
	manifests := loadApplicationDataFiles()
	if len(manifests) > 0 {
		t.Log("Success")
		t.Log(manifests)
	} else {
		t.Error("Error")
	}
}

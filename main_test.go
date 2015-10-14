package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"

	"testing"

	"github.com/samalba/dockerclient"
)

var bitbucketObject BitbucketPayload
var sApplicationData ApplicationData

func init() {
	byteArray, err := ioutil.ReadFile("bitbucket.push.json")
	if err != nil {
		panic(err)
	}

	json.Unmarshal(byteArray, &bitbucketObject)
	ConfDirectory = "./testConf"

	ApplicationDataPath = "./applicationDataFiles"

	docker, _ = dockerclient.NewDockerClient("unix:///var/run/docker.sock", nil)

	manifests := loadApplicationDataFiles()

	bitbucketObject.Repository.Name = "test"

	for _, manifest := range manifests {
		if strings.ToLower(manifest.Name) == strings.ToLower(bitbucketObject.GetRepositoryName()) {
			sApplicationData = manifest
		}
	}

	//Start nginx
	// nginxTest := sApplicationData
	// nginxTest.Name = "ambassador_webserver"
	// nginxTest.Image = "nginx:latest"
	// runContainer(nginxTest)
}
func TestBitbucketPushLookup(t *testing.T) {

	if bitbucketObject.Repository.Name == "test" {
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
	fmt.Print(containers)
	if len(containers) > 0 {
		t.Log("Success")
	} else {
		t.Error("Could not get any containers")
	}
}

//TODO:Skipping till I hear an answer for
//https://github.com/samalba/dockerclient/issues/173
func sTestExec(t *testing.T) {
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

func TestRunDockerExec(t *testing.T) {
	var containerID string
	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {

			if strings.Contains(name, WebserverDockerName) == true {
				fmt.Println(name, WebserverDockerName)
				containerID = c.Id
			}
		}
	}

	if containerID == "" {
		t.Error("Could not find ", WebserverDockerName)
	}

	reloadCommand := exec.Command("docker", "exec", "-t", containerID, "nginx", "-s", "reload")
	output, err := reloadCommand.CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}

	if strings.Contains(string(output), "signal process started") == true {
		t.Log("Success")
	} else {
		t.Error("Error")
	}
}

//Build the image and make sure the container running runs
//only after the image is finish building
func TestBuildImage(t *testing.T) {
	// TODO: tried using api
	// var reponame = "testyimage:latest"
	// TODO: build image
	sApplicationData.DockerfilePath = "testdockerfiledirectory"
	sApplicationData.Image = "busybox"
	sApplicationData.Dockerfilename = "Dockerfile.busybox"
	buildImageViaCLI(&sApplicationData)

	//TODO: Run Container
	ContainerInfo := runContainer(sApplicationData)

	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		log.Fatalf("cannot get containers: %s", err)
	}
	var found bool
	//Only find applications with the same name
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.Contains(name, sApplicationData.Name) == true {
				found = true
			}
		}
	}

	if found {
		t.Log("Success Found Container: ", ContainerInfo.Id)
	} else {
		t.Error("Error running container after building image")
	}
}

func TestMakeDockerfileTar(t *testing.T) {

	path := "testdockerfiledirectory"

	Makedockerfiletar(path)

	if _, err := os.Stat(fmt.Sprintf("%s/%s", path, "Dockerfile.tar")); err == nil {
		t.Log("Success")
	} else {
		t.Error("Dockerfile tar does not exists")
	}
}

func TestGenContainer(t *testing.T) {
	c := runContainer(sApplicationData)
	var foundVolume bool

	for k, v := range c.Volumes {
		for _, t := range sApplicationData.VolumeBinds {
			fmt.Println(fmt.Sprintf("%s:%s", v, k), t)
			if fmt.Sprintf("%s:%s", v, k) == t {
				foundVolume = true
			}
		}
	}

	if foundVolume {
		t.Log("Success")
	} else {
		t.Error("Failed to installed appropriate volumes")
	}
	var port, ip string
	for portString, portBinding := range c.NetworkSettings.Ports {
		if portString == "80/tcp" {
			for _, binding := range portBinding {
				port = binding.HostPort
				ip = c.NetworkSettings.IPAddress
			}
		}
	}
	updateApplicationCurrentPort(&sApplicationData, c)
	UpdateApplicationNginxConf(sApplicationData)

	b, err := ioutil.ReadFile("testConf/test.conf")
	if err != nil {
		panic(err)
	}

	if strings.Contains(string(b), fmt.Sprintf("%s:%s", ip, port)) == false {
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
	} else {
		t.Error("Error")
	}
	fmt.Println(manifests)

}

func TestReadTestImage(t *testing.T) {

	imageName := getImageFromDockerfile("testdockerfiledirectory/Dockerfile.busybox")

	if imageName == "busybox" {
		t.Log("Success")
	} else {
		t.Error("Failed to find correct image", imageName)
	}

}
func TestApplicationTest(t *testing.T) {
	//Check for hasTest flag
	//Build the image test docker file path
	//Run Container
	branchName := "testybaby"

	sApplicationData.TestDockerfilepath = "testdockerfiledirectory"
	sApplicationData.Dockerfilename = "Dockerfile.busybox"
	sApplicationData.Name = "test" // Repository Name
	sApplicationData.Image = "busybox:latest"
	bitbucketObject.SetBranchName(branchName)
	bitbucketObject.SetRepositoryName("test")
	sApplicationData.Command = []string{"/bin/sh", "-c", "while :; do echo 'Hit CTRL+C'; sleep 1; done"}
	TestApplication(sApplicationData, bitbucketObject)

	dockerFilePath := fmt.Sprintf("%s/%s", sApplicationData.DockerfilePath, "Dockerfile")

	byteArray, err := ioutil.ReadFile(dockerFilePath)
	if err != nil {
		t.Error(err)
	}

	if strings.Contains(string(byteArray), fmt.Sprintf("git clone -b %s", branchName)) {
		t.Log("Success")
	} else {
		t.Error("Error")
	}

	containers, err := docker.ListContainers(true, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}

	var found bool
	for _, c := range containers {
		for _, name := range c.Names {

			if strings.Contains(name, fmt.Sprintf("%s-test-", sApplicationData.Name)) == true {
				found = true
			}
		}
	}

	if found == false {
		t.Error("Could not find container")
	}
}

func TestStopOldContainers(t *testing.T) {
	containerName := "reponame"
	sApplicationData.Name = containerName
	sApplicationData.Image = "registry:2"
	//Run two identical containers
	runContainer(sApplicationData)
	cInfo := runContainer(sApplicationData)

	StopOldContainers(sApplicationData, cInfo)

	containers, err := docker.ListContainers(false, false, "")
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}

	containerNames := []string{}
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.Contains(name, containerName) == true {
				containerNames = append(containerNames, name)
			}
		}
	}

	if len(containerNames) > 1 {
		t.Error("Still numerous containers still alive")
	} else {
		t.Log("Success")
	}
}
